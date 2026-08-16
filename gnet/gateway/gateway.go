// Package gateway 提供 L4 网关：规则路由 + TCP/UDP 端口转发，复用
// forward 的桥接引擎（TCP 半关闭、协议感知钩子）。L3/TUN 用户态栈网关
// 规划于 M5+（可选集成 gVisor netstack，需单独评审）。
//
// Package gateway provides an L4 gateway: rule routing plus TCP/UDP port
// forwarding, reusing the forward bridging engines (TCP half-close,
// protocol-aware hooks). An L3/TUN userspace-stack gateway is planned for
// M5+ (optional gVisor netstack integration, pending review).
package gateway

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/sofiworker/gk/gnet/forward"
)

// Rule 是端口转发规则。
// Rule is one port-forwarding rule.
type Rule struct {
	// Listen 本地监听地址（如 ":8080"）。
	Listen string
	// Upstream 转发目标（如 "10.0.0.1:80"）。
	Upstream string
	// Protocol：tcp（默认）或 udp。
	Protocol string
}

// Gateway 是 L4 网关。
// Gateway is the L4 gateway.
type Gateway struct {
	rules []Rule

	logger  *log.Logger
	handler forward.ProtocolHandler
	bufSize int
	timeout time.Duration

	mu     sync.Mutex
	lns    []net.Listener
	udps   []*net.UDPConn
	closed bool
	wg     sync.WaitGroup
}

// Option 配置网关。
// Option configures the gateway.
type Option func(*Gateway)

// WithLogger 设置日志器。
// WithLogger sets the logger.
func WithLogger(l *log.Logger) Option {
	return func(g *Gateway) {
		if l != nil {
			g.logger = l
		}
	}
}

// WithProtocolHandler 为全部 TCP 转发设置协议感知改写钩子
// （大小端转换等，见 forward.ProtocolHandler）。
//
// WithProtocolHandler sets the protocol-aware rewrite hook for all TCP
// forwards (byte-order conversion etc., see forward.ProtocolHandler).
func WithProtocolHandler(h forward.ProtocolHandler) Option {
	return func(g *Gateway) { g.handler = h }
}

// WithBufferSize 设置 TCP 桥接缓冲大小。
// WithBufferSize sets the TCP bridge buffer size.
func WithBufferSize(n int) Option {
	return func(g *Gateway) {
		if n > 0 {
			g.bufSize = n
		}
	}
}

// WithTimeout 设置 TCP 桥接读超时。
// WithTimeout sets the TCP bridge read timeout.
func WithTimeout(d time.Duration) Option {
	return func(g *Gateway) { g.timeout = d }
}

// New 创建网关（规则在 Run 时生效）。
// New creates a gateway (rules take effect on Run).
func New(rules []Rule, opts ...Option) *Gateway {
	g := &Gateway{rules: rules, logger: log.Default()}
	for _, o := range opts {
		o(g)
	}
	return g
}

// Run 启动全部规则的监听并阻塞，直至 ctx 取消或 Stop。
// Run starts listeners for all rules and blocks until ctx cancellation or
// Stop.
func (g *Gateway) Run(ctx context.Context) error {
	for _, rule := range g.rules {
		switch {
		case rule.Protocol == "" || rule.Protocol == "tcp":
			ln, err := net.Listen("tcp", rule.Listen)
			if err != nil {
				g.closeAll()
				return fmt.Errorf("gateway: listen %s: %w", rule.Listen, err)
			}
			g.mu.Lock()
			g.lns = append(g.lns, ln)
			g.mu.Unlock()
			g.wg.Add(1)
			go g.serveTCP(ctx, ln, rule.Upstream)
		case rule.Protocol == "udp":
			addr, err := net.ResolveUDPAddr("udp", rule.Listen)
			if err != nil {
				g.closeAll()
				return fmt.Errorf("gateway: resolve %s: %w", rule.Listen, err)
			}
			uc, err := net.ListenUDP("udp", addr)
			if err != nil {
				g.closeAll()
				return fmt.Errorf("gateway: listen %s: %w", rule.Listen, err)
			}
			g.mu.Lock()
			g.udps = append(g.udps, uc)
			g.mu.Unlock()
			g.wg.Add(1)
			go g.serveUDP(ctx, uc, rule.Upstream)
		default:
			g.closeAll()
			return fmt.Errorf("gateway: unknown protocol %q", rule.Protocol)
		}
	}
	<-ctx.Done()
	g.closeAll()
	g.wg.Wait()
	if err := ctx.Err(); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

// Stop 关闭全部监听并等待转发结束。
// Stop closes all listeners and waits for forwards to finish.
func (g *Gateway) Stop(ctx context.Context) error {
	g.closeAll()
	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// serveTCP 接受连接并经 forward 桥接转发到上游。
// serveTCP accepts conns and forwards them upstream via the forward bridge.
func (g *Gateway) serveTCP(ctx context.Context, ln net.Listener, upstream string) {
	defer g.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // 监听已关闭
		}
		go g.forwardTCP(upstream, conn)
	}
}

// forwardTCP 建立上游连接并桥接。
// forwardTCP dials the upstream and bridges.
func (g *Gateway) forwardTCP(upstream string, local net.Conn) {
	remote, err := net.DialTimeout("tcp", upstream, 10*time.Second)
	if err != nil {
		g.logger.Printf("gateway: dial %s: %v", upstream, err)
		_ = local.Close()
		return
	}
	bridge := forward.NewProtocolAwareBridge(local, remote, forward.TCPConfig{
		BufferSize: g.bufSize,
		Logger:     g.logger,
		Timeout:    g.timeout,
	}, &forward.ByteOrderConfig{ProtocolHandler: g.handler})
	_ = bridge.Start()
}

// serveUDP 用 forward 的 UDP 桥接转发。
// serveUDP forwards via the forward UDP bridge.
func (g *Gateway) serveUDP(ctx context.Context, uc *net.UDPConn, upstream string) {
	defer g.wg.Done()
	bridge, err := forward.NewUDPBridge(uc, upstream)
	if err != nil {
		g.logger.Printf("gateway: udp bridge: %v", err)
		return
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = bridge.Close()
		case <-done:
		}
	}()
	_ = bridge.Start()
	close(done)
}

// closeAll 关闭全部监听。
// closeAll closes every listener.
func (g *Gateway) closeAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	for _, ln := range g.lns {
		_ = ln.Close()
	}
	for _, uc := range g.udps {
		_ = uc.Close()
	}
}

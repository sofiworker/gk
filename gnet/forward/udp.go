package forward

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// UDPSession 表示一个UDP会话：每个会话持有独立的本地 upstream socket，
// 使 remote 回包可经该 socket 归属回对应 client（真正的双向桥接）。
type UDPSession struct {
	localAddr  *net.UDPAddr
	remoteAddr *net.UDPAddr
	upstream   *net.UDPConn // 本会话专属上游 socket
	lastActive time.Time
	dataChan   chan []byte
	closeChan  chan struct{}
	closeOnce  sync.Once
}

// UDPBridge UDP双向桥接
type UDPBridge struct {
	localConn  *net.UDPConn
	remoteAddr *net.UDPAddr
	bufferSize int
	logger     *log.Logger
	mu         sync.RWMutex
	closed     bool
	sessions   map[string]*UDPSession // key: client_addr->server_addr
	sessionTTL time.Duration
}

// UDPConfig UDP桥接配置
type UDPConfig struct {
	BufferSize int           // 缓冲区大小
	Logger     *log.Logger   // 日志记录器
	SessionTTL time.Duration // 会话超时时间
}

// NewUDPBridge 创建新的UDP桥接实例
func NewUDPBridge(localConn *net.UDPConn, remoteAddr string, config ...UDPConfig) (*UDPBridge, error) {
	// 解析远程地址
	raddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve remote address: %v", err)
	}

	bridge := &UDPBridge{
		localConn:  localConn,
		remoteAddr: raddr,
		bufferSize: 4096,
		sessions:   make(map[string]*UDPSession),
		sessionTTL: 5 * time.Minute, // 默认5分钟超时
	}

	// 应用配置
	if len(config) > 0 {
		cfg := config[0]
		if cfg.BufferSize > 0 {
			bridge.bufferSize = cfg.BufferSize
		}
		bridge.logger = cfg.Logger
		if cfg.SessionTTL > 0 {
			bridge.sessionTTL = cfg.SessionTTL
		}
	}

	// 如果没有提供日志记录器，使用默认的
	if bridge.logger == nil {
		bridge.logger = log.Default()
	}

	return bridge, nil
}

// Start 启动UDP双向桥接
func (b *UDPBridge) Start() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return io.ErrClosedPipe
	}
	b.mu.Unlock()

	b.logger.Printf("Starting UDP bridge: %s -> %s", b.localConn.LocalAddr(), b.remoteAddr)

	// 启动会话清理器
	go b.sessionCleaner()

	// 启动数据接收循环
	for {
		b.mu.RLock()
		if b.closed {
			b.mu.RUnlock()
			return nil
		}
		b.mu.RUnlock()

		buffer := make([]byte, b.bufferSize)
		n, clientAddr, err := b.localConn.ReadFromUDP(buffer)
		if err != nil {
			b.mu.RLock()
			closed := b.closed
			b.mu.RUnlock()

			if closed {
				return nil
			}
			b.logger.Printf("ReadFromUDP error: %v", err)
			continue
		}

		if n == 0 {
			continue
		}

		b.logger.Printf("Received %d bytes from %s", n, clientAddr)

		// 处理接收到的数据
		go b.handleIncomingData(buffer[:n], clientAddr)
	}
}

// handleIncomingData 处理接收到的数据
func (b *UDPBridge) handleIncomingData(data []byte, clientAddr *net.UDPAddr) {
	sessionKey := fmt.Sprintf("%s->%s", clientAddr.String(), b.remoteAddr.String())

	b.mu.Lock()
	session, exists := b.sessions[sessionKey]
	if !exists {
		// 会话专属上游 socket：随机本地端口，回包归属清晰。
		upstream, err := net.ListenUDP("udp", nil)
		if err != nil {
			b.mu.Unlock()
			b.logger.Printf("UDP upstream socket failed: %v", err)
			return
		}
		// 创建新会话
		session = &UDPSession{
			localAddr:  clientAddr,
			remoteAddr: b.remoteAddr,
			upstream:   upstream,
			lastActive: time.Now(),
			dataChan:   make(chan []byte, 100), // 缓冲通道
			closeChan:  make(chan struct{}),
		}
		b.sessions[sessionKey] = session

		// 启动远程数据接收（转发与回包）
		go b.startRemoteReceiver(session)

		b.logger.Printf("New UDP session created: %s", sessionKey)
	} else {
		session.lastActive = time.Now()
	}
	b.mu.Unlock()

	// 发送数据到远程
	if exists {
		select {
		case session.dataChan <- data:
			// 数据成功发送到通道
		default:
			b.logger.Printf("Session channel full, dropping data: %s", sessionKey)
		}
	} else {
		// 对于新会话，直接发送第一包数据
		go b.forwardToRemote(data, session)
	}
}

// forwardToRemote 转发数据到远程（经会话专属 upstream，源端口稳定）。
func (b *UDPBridge) forwardToRemote(data []byte, session *UDPSession) {
	_, err := session.upstream.WriteToUDP(data, session.remoteAddr)
	if err != nil {
		b.logger.Printf("WriteToUDP error to %s: %v", session.remoteAddr, err)
		return
	}
	b.logger.Printf("Forwarded %d bytes to %s", len(data), session.remoteAddr)
}

// startRemoteReceiver 启动远程数据接收器：转发 client→remote 数据，
// 并把 remote 回包（经会话专属 upstream）送回对应 client。
func (b *UDPBridge) startRemoteReceiver(session *UDPSession) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer b.closeSession(session)

	// 回包接收循环：upstream → client。
	go func() {
		buf := make([]byte, b.bufferSize)
		for {
			n, src, err := session.upstream.ReadFromUDP(buf)
			if err != nil {
				return
			}
			// 只接受来自 remote 的回包。
			if !src.IP.Equal(b.remoteAddr.IP) || src.Port != b.remoteAddr.Port {
				continue
			}
			if _, err := b.localConn.WriteToUDP(buf[:n], session.localAddr); err != nil {
				b.logger.Printf("UDP reply to client failed: %v", err)
				return
			}
			b.mu.Lock()
			session.lastActive = time.Now()
			b.mu.Unlock()
		}
	}()

	for {
		select {
		case data := <-session.dataChan:
			b.forwardToRemote(data, session)
			b.mu.Lock()
			session.lastActive = time.Now()
			b.mu.Unlock()

		case <-ticker.C:
			b.mu.RLock()
			expired := time.Since(session.lastActive) > b.sessionTTL
			b.mu.RUnlock()
			if expired {
				b.logger.Printf("UDP session expired: %s->%s", session.localAddr, session.remoteAddr)
				return
			}

		case <-session.closeChan:
			return
		}
	}
}

// closeSession 幂等清理会话：关 upstream、从表移除。
// closeSession idempotently tears down a session: closes the upstream and
// removes it from the table.
func (b *UDPBridge) closeSession(session *UDPSession) {
	session.closeOnce.Do(func() {
		close(session.closeChan)
		_ = session.upstream.Close()
	})
	b.mu.Lock()
	delete(b.sessions, fmt.Sprintf("%s->%s", session.localAddr.String(), session.remoteAddr.String()))
	b.mu.Unlock()
}

// sessionCleaner 清理过期会话
func (b *UDPBridge) sessionCleaner() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}

		now := time.Now()
		expiredSessions := make([]string, 0)

		for key, session := range b.sessions {
			if now.Sub(session.lastActive) > b.sessionTTL {
				expiredSessions = append(expiredSessions, key)
				close(session.closeChan)
			}
		}

		for _, key := range expiredSessions {
			delete(b.sessions, key)
			b.logger.Printf("Cleaned expired session: %s", key)
		}
		b.mu.Unlock()
	}
}

// Close 关闭UDP桥接
func (b *UDPBridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	sessions := make([]*UDPSession, 0, len(b.sessions))
	for key, session := range b.sessions {
		sessions = append(sessions, session)
		delete(b.sessions, key)
	}
	b.mu.Unlock()
	b.logger.Println("Closing UDP bridge...")

	// 关闭所有会话（closeOnce 保证幂等，closeChan 只关一次）。
	for _, session := range sessions {
		session.closeOnce.Do(func() {
			close(session.closeChan)
			_ = session.upstream.Close()
		})
	}

	if b.localConn != nil {
		return b.localConn.Close()
	}

	return nil
}

// GetActiveSessions 获取活动会话数量
func (b *UDPBridge) GetActiveSessions() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.sessions)
}

package ghttp

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ===========================================================================
// 优雅退出编排 / Graceful shutdown orchestration
//
// RunGraceful 是纯组合的便利层,构建在既有 Run + Shutdown + ReadinessGate 之上,不改动
// 它们的语义。它把生产上常见的 "SIGTERM → 摘流 → 等待 LB 感知 → 排水 → 超时强关" 编排
// 成一次调用,全部用标准库 os/signal + signal.NotifyContext(Go 1.16+)实现,零第三方依赖。
// 仅生命周期方法,不在请求路径,无性能影响。
//
// RunGraceful is a pure-composition convenience layer over the existing Run +
// Shutdown + ReadinessGate, leaving their semantics unchanged. It orchestrates the
// common production sequence "SIGTERM → drain readiness → wait for LB → drain
// in-flight → force-close on timeout" into one call, using only the standard
// library os/signal + signal.NotifyContext (Go 1.16+), with zero third-party deps.
// It is a lifecycle method off the request path with no performance impact.
// ===========================================================================

// DefaultShutdownTimeout 是 RunGraceful 排水阶段的默认超时。
// DefaultShutdownTimeout is the default drain timeout for RunGraceful.
const DefaultShutdownTimeout = 30 * time.Second

// gracefulConfig 汇集 RunGraceful 的可调项,由 GracefulOption 填充。
// gracefulConfig gathers RunGraceful's tunables, populated by GracefulOption.
type gracefulConfig struct {
	drainDelay      time.Duration
	shutdownTimeout time.Duration
	gate            *ReadinessGate
	signals         []os.Signal
}

// GracefulOption 配置 RunGraceful 的一次调用。
// GracefulOption configures one RunGraceful call.
type GracefulOption func(*gracefulConfig)

// WithDrainDelay 设置摘流后、真正排水前的等待时长,给负载均衡器时间感知本实例已不就绪。
// 默认 0(不等待)。
// WithDrainDelay sets the wait between marking not-ready and starting the drain,
// giving the load balancer time to notice this instance is unready. Default 0.
func WithDrainDelay(d time.Duration) GracefulOption {
	return func(c *gracefulConfig) { c.drainDelay = d }
}

// WithShutdownTimeout 设置排水阶段的超时;超时后 RunGraceful 调 Close 强制关闭。
// 默认 DefaultShutdownTimeout(30s)。传入 <=0 视为使用默认值。
// WithShutdownTimeout sets the drain-phase timeout; on timeout RunGraceful calls
// Close to force-stop. Default DefaultShutdownTimeout (30s). A value <=0 uses the
// default.
func WithShutdownTimeout(d time.Duration) GracefulOption {
	return func(c *gracefulConfig) { c.shutdownTimeout = d }
}

// WithReadinessGate 绑定一个就绪门闸:收到停止信号后 RunGraceful 先 Set(false) 让健康检查
// 转为未就绪,使负载均衡器摘流,再开始排水。默认不绑定(跳过摘流步骤)。
// WithReadinessGate binds a readiness gate: on a stop signal RunGraceful first
// Set(false) so the health check turns not-ready and the load balancer drains
// traffic, then starts draining. Unbound by default (the drain-readiness step is
// skipped).
func WithReadinessGate(g *ReadinessGate) GracefulOption {
	return func(c *gracefulConfig) { c.gate = g }
}

// WithSignals 覆盖触发优雅退出的信号集。默认 SIGINT、SIGTERM。
// WithSignals overrides the signal set that triggers graceful shutdown. Default
// SIGINT, SIGTERM.
func WithSignals(sig ...os.Signal) GracefulOption {
	return func(c *gracefulConfig) { c.signals = sig }
}

// RunGraceful 在 addr 上启动服务并阻塞,直到收到停止信号(默认 SIGINT/SIGTERM)后编排优雅
// 退出:①若绑定了 ReadinessGate,先 Set(false) 让 LB 摘流;②等待 drainDelay;③以
// shutdownTimeout 调 Shutdown 排水;④超时则 Close 强制关闭。
//
// 返回值:正常优雅关闭返回 nil(已把 Run 的 ErrServerClosed 归一为 nil);监听失败(如端口
// 被占用)在启动阶段即返回该错误,不进入信号等待。
//
// RunGraceful starts the server on addr and blocks until a stop signal (default
// SIGINT/SIGTERM), then orchestrates graceful shutdown: (1) if a ReadinessGate is
// bound, Set(false) so the LB drains; (2) wait drainDelay; (3) Shutdown with
// shutdownTimeout; (4) Close on timeout.
//
// Return: a clean shutdown returns nil (Run's ErrServerClosed is normalized to
// nil); a listen failure (e.g. port in use) returns that error during startup
// without entering the signal wait.
func (s *Server) RunGraceful(addr string, opts ...GracefulOption) error {
	cfg := gracefulConfig{shutdownTimeout: DefaultShutdownTimeout}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.shutdownTimeout <= 0 {
		cfg.shutdownTimeout = DefaultShutdownTimeout
	}
	sig := cfg.signals
	if len(sig) == 0 {
		sig = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}

	// 用 NotifyContext 监听信号:ctx.Done() 在收到信号时触发。stop 释放信号订阅。
	// Watch signals via NotifyContext: ctx.Done() fires on a signal. stop releases
	// the signal subscription.
	ctx, stop := signal.NotifyContext(context.Background(), sig...)
	defer stop()

	// Run 阻塞,放到 goroutine;监听失败或正常关闭都经 runErr 回传。
	// Run blocks, so run it in a goroutine; a listen failure or clean shutdown
	// propagates via runErr.
	runErr := make(chan error, 1)
	go func() { runErr <- s.Run(addr) }()

	select {
	case err := <-runErr:
		// 未等到信号 Run 就返回:通常是监听失败(端口占用/权限)。直接返回该结果。
		// Run returned before any signal: usually a listen failure (port in use /
		// permissions). Return that outcome directly.
		return err
	case <-ctx.Done():
		// 收到停止信号,进入优雅退出编排。
		// Stop signal received; enter the graceful-shutdown orchestration.
		stop() // 尽早恢复默认信号处理:再来一次信号即可强制退出。restore default handling so a second signal force-quits.
		return s.gracefulStop(&cfg, runErr)
	}
}

// gracefulStop 执行摘流→等待→排水→(超时)强关,并汇合 Run 的最终返回值。
// gracefulStop performs drain-readiness → wait → drain → (timeout) force-close,
// then joins Run's final return value.
func (s *Server) gracefulStop(cfg *gracefulConfig, runErr <-chan error) error {
	// ① 摘流:置就绪门闸为未就绪,让 LB 停止转发新流量。
	// (1) Drain readiness: mark the gate not-ready so the LB stops new traffic.
	if cfg.gate != nil {
		cfg.gate.Set(false, ErrShuttingDown)
	}
	// ② 等待 LB 感知。
	// (2) Wait for the LB to notice.
	if cfg.drainDelay > 0 {
		time.Sleep(cfg.drainDelay)
	}
	// ③ 限时排水。
	// (3) Bounded drain.
	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	defer cancel()
	err := s.Shutdown(shutCtx)
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		// ④ 排水超时:强制关闭,中断残留连接。
		// (4) Drain timed out: force-close, interrupting lingering connections.
		_ = s.Close()
	}
	// 汇合 Run 的返回:正常关闭它返回 ErrServerClosed,这里归一为 nil;排水阶段本身
	// 的非超时错误优先返回。
	// Join Run's return: on a clean shutdown it returns ErrServerClosed, normalized
	// to nil here; a non-timeout error from the drain itself takes precedence.
	runResult := <-runErr
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if runResult != nil && !errors.Is(runResult, ErrServerClosed) {
		return runResult
	}
	return nil
}

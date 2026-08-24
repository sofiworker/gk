package app

import (
	"net/http"

	"github.com/sofiworker/gk/ghttp"
)

// New 创建测试应用处理器和清理函数。
// New creates the test application handler and cleanup function.
func New(cfg Config) (http.Handler, func()) {
	store := NewStore()
	state := newStateController(store)
	metrics := NewRuntimeMetrics()
	faults := NewFaultController()
	server := ghttp.New()
	server.Use(observeRequests(metrics))
	server.Use(globalTraceMiddleware)
	server.Use(ghttp.Recovery())
	registerRouting(server, state, metrics, faults)
	registerState(server, state)
	registerBinding(server, cfg)
	registerOutput(server)
	registerErrors(server, cfg)
	registerFaults(server, cfg, faults)
	registerTyped(server)
	return server, func() {
		state.reset()
		metrics.Reset()
		faults.Reset()
	}
}

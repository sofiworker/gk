package app

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/sofiworker/gk/ghttp"
)

var requestIDSequence atomic.Uint64

// observeRequests 为每个请求注入 X-Request-ID 并记录请求指标。ghttp.Response 原生
// 追踪状态码与输出字节数,无需再包装 writer。
// observeRequests injects X-Request-ID on every request and records request
// metrics. ghttp.Response natively tracks the status code and bytes written, so
// no writer wrapping is needed.
func observeRequests(metrics *RuntimeMetrics) ghttp.Middleware {
	return func(next ghttp.Handler) ghttp.Handler {
		return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
			requestID := req.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = fmt.Sprintf("k6-%016x", requestIDSequence.Add(1))
			}
			resp.Header().Set("X-Request-ID", requestID)
			// 指标端点不参与自身统计,否则 active_requests 恒含本次快照请求。
			// The metrics endpoint is excluded from its own accounting, otherwise
			// active_requests always includes the snapshotting request itself.
			if req.URL.Path == "/__test/metrics" {
				return next(ctx, req, resp)
			}
			finish := metrics.BeginRequest()
			err := next(ctx, req, resp)
			status := resp.Status()
			if status == 0 {
				status = http.StatusOK
			}
			requestBytes := req.ContentLength
			if requestBytes < 0 {
				requestBytes = 0
			}
			responseBytes := resp.BytesOut()
			if !responseCanHaveBody(req.Method, status) {
				responseBytes = 0
			}
			finish(status, uint64(requestBytes), uint64(responseBytes))
			return err
		}
	}
}

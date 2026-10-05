package ghttpotel

import (
	"context"
	"time"

	"github.com/sofiworker/gk/ghttp"
	"github.com/sofiworker/gk/ghttp/wire"
	"github.com/sofiworker/gk/gotel"
)

// 指标名（OTel 语义约定）。
// Metric names (OTel semantic conventions).
const (
	MetricRequestDuration = "http.server.request.duration"
	MetricActiveRequests  = "http.server.active_requests"
	MetricRequestBody     = "http.server.request.body.size"
	MetricResponseBody    = "http.server.response.body.size"
)

// Metrics 返回指标中间件。属性仅含 method、route、status_code（低基数）；未匹配路由不带 http.route。
// 活跃请求数用 Counter 的 +1/-1 表示（gotel 的 Gauge 只有 Record，无法原子增减）。meter 为 nil 时透传。
// Metrics returns a metrics middleware. Attributes are limited to method, route and
// status_code (low cardinality); unmatched routes omit http.route. Active requests use a
// Counter with +1/-1 since the gotel Gauge only supports Record (no atomic add). A nil
// meter passes through.
func Metrics(meter gotel.Meter, opts ...Option) ghttp.Middleware {
	c := newConfig(opts)
	return func(next ghttp.Handler) ghttp.Handler {
		if meter == nil {
			return next
		}
		duration := meter.Histogram(MetricRequestDuration, "s")
		var active gotel.Counter
		if c.activeReqs {
			active = meter.Counter(MetricActiveRequests)
		}
		var reqBody, respBody gotel.Histogram
		if c.bodySizes {
			reqBody = meter.Histogram(MetricRequestBody, "By")
			respBody = meter.Histogram(MetricResponseBody, "By")
		}
		return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
			base := []gotel.KeyValue{gotel.KV(AttrMethod, wire.LogToken(req.Raw.Method))}
			if route := req.Route(); route != "" {
				base = append(base, gotel.KV(AttrRoute, route))
			}
			if active != nil {
				active.Add(ctx, 1, base...)
				defer active.Add(ctx, -1, base...)
			}
			start := time.Now()
			err := next(ctx, req, resp)

			attrs := append(base[:len(base):len(base)],
				gotel.KV(AttrStatusCode, int64(ghttp.FinalStatus(resp, err))))
			duration.Record(ctx, time.Since(start).Seconds(), attrs...)
			if reqBody != nil && req.Raw.ContentLength >= 0 {
				reqBody.Record(ctx, float64(req.Raw.ContentLength), attrs...)
			}
			if respBody != nil {
				respBody.Record(ctx, float64(resp.Size()), attrs...)
			}
			return err
		}
	}
}

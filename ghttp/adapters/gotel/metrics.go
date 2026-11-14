package ghttpotel

import (
	"context"
	"fmt"
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
// 活跃请求数用 UpDownCounter 的 +1/-1 表示。指标在初始化阶段创建；meter 为 nil 时透传。
// Metrics returns a metrics middleware. Attributes are limited to method, route and
// status_code (low cardinality); unmatched routes omit http.route. Active requests use a
// UpDownCounter with +1/-1. Instruments are created before serving; a nil meter
// passes through.
func Metrics(meter gotel.Meter, opts ...Option) (ghttp.Middleware, error) {
	c := newConfig(opts)
	if meter == nil {
		return func(next ghttp.Handler) ghttp.Handler { return next }, nil
	}
	attrs := gotel.WithAttributeKeys(AttrMethod, AttrRoute, AttrStatusCode)
	duration, err := meter.Histogram(MetricRequestDuration, gotel.WithUnit("s"), attrs)
	if err != nil {
		return nil, fmt.Errorf("ghttpotel: create request duration: %w", err)
	}
	var active gotel.UpDownCounter
	if c.activeReqs {
		active, err = meter.UpDownCounter(MetricActiveRequests, gotel.WithAttributeKeys(AttrMethod, AttrRoute))
		if err != nil {
			return nil, fmt.Errorf("ghttpotel: create active requests: %w", err)
		}
	}
	var reqBody, respBody gotel.Histogram
	if c.bodySizes {
		reqBody, err = meter.Histogram(MetricRequestBody, gotel.WithUnit("By"), attrs)
		if err != nil {
			return nil, fmt.Errorf("ghttpotel: create request body size: %w", err)
		}
		respBody, err = meter.Histogram(MetricResponseBody, gotel.WithUnit("By"), attrs)
		if err != nil {
			return nil, fmt.Errorf("ghttpotel: create response body size: %w", err)
		}
	}
	return func(next ghttp.Handler) ghttp.Handler {
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
	}, nil
}

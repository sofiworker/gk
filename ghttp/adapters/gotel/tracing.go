package ghttpotel

import (
	"context"
	"net"
	"net/http"

	"github.com/sofiworker/gk/ghttp"
	"github.com/sofiworker/gk/ghttp/wire"
	"github.com/sofiworker/gk/gotel"
)

// OTel 语义约定属性名。
// OTel semantic-convention attribute names.
const (
	AttrMethod     = "http.request.method"
	AttrRoute      = "http.route"
	AttrURLPath    = "url.path"
	AttrStatusCode = "http.response.status_code"
	AttrServerAddr = "server.address"
)

// UnmatchedRoute 是未匹配路由在 span 名中使用的占位，避免高基数。
// UnmatchedRoute is the placeholder used in span names for unmatched routes (low cardinality).
const UnmatchedRoute = "unmatched"

// serverAddress 返回 server.address：优先配置值，否则取 Host 去端口并净化。
// serverAddress returns server.address: the configured value, else Host without port, sanitized.
func serverAddress(c config, r *http.Request) string {
	if c.serverAddress != "" {
		return c.serverAddress
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return wire.LogToken(host)
}

// Tracing 返回追踪中间件：提取上游上下文、开启 server span、把 span ctx 传给后续处理链。
// 状态码 >=500 或 handler 返回错误时 span 标记为 error 并记录错误。tracer 为 nil 时透传。
// Tracing returns a tracing middleware: it extracts the upstream context, starts a span and
// passes the span ctx down the chain. A status >=500 or a handler error marks the span as
// error and records it. A nil tracer passes through.
func Tracing(tracer gotel.Tracer, opts ...Option) ghttp.Middleware {
	c := newConfig(opts)
	return func(next ghttp.Handler) ghttp.Handler {
		if tracer == nil {
			return next
		}
		return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
			method := wire.LogToken(req.Raw.Method)
			route := req.Route()
			name := method + " " + UnmatchedRoute
			if route != "" {
				name = method + " " + route
			}

			ctx, _ = tracer.Extract(ctx, HeaderCarrier(req.Raw.Header))
			ctx, span := tracer.Start(ctx, name)
			defer span.End()

			attrs := []gotel.KeyValue{
				gotel.KV(AttrMethod, method),
				gotel.KV(AttrURLPath, wire.LogToken(req.Raw.URL.Path)),
				gotel.KV(AttrServerAddr, serverAddress(c, req.Raw)),
			}
			if route != "" {
				attrs = append(attrs, gotel.KV(AttrRoute, route))
			}
			span.SetAttributes(attrs...)

			if c.traceIDHeader != "" && !resp.Written() {
				if sc := span.Context(); sc != nil && sc.TraceID() != "" {
					resp.Header().Set(c.traceIDHeader, sc.TraceID())
				}
			}

			req.Raw = req.Raw.WithContext(ctx)
			err := next(ctx, req, resp)

			status := ghttp.FinalStatus(resp, err)
			span.SetAttributes(gotel.KV(AttrStatusCode, int64(status)))
			if err != nil {
				span.RecordError(err)
			}
			if err != nil || status >= 500 {
				desc := http.StatusText(status)
				if err != nil {
					desc = err.Error()
				}
				span.SetStatus(gotel.StatusCodeError, desc)
			}
			return err
		}
	}
}

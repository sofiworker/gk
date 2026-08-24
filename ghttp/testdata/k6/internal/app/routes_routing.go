package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/sofiworker/gk/ghttp"
)

func registerRouting(server *ghttp.Server, state *stateController, metrics *RuntimeMetrics, faults *FaultController) {
	rawJSON := func(value any) ghttp.RawHandlerFunc {
		return rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, value)
		}))
	}

	// headOnlyJSON 是 HEAD 探针:只写 Content-Type 与状态,不写 body。
	// headOnlyJSON is a HEAD probe: writes Content-Type and status only, no body.
	headOnlyJSON := func() ghttp.RawHandlerFunc {
		return rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
		}))
	}

	mustRaw(server.RawHandle(http.MethodGet, "/health", rawJSON(map[string]string{"status": "ok"})))
	mustRaw(server.RawHandle(http.MethodHead, "/health", headOnlyJSON()))
	mustRaw(server.RawHandle(http.MethodGet, "/health/", rawJSON(map[string]string{"status": "ok"})))
	mustRaw(server.RawHandle(http.MethodGet, "/ready", rawJSON(map[string]string{"status": "ready"})))
	mustRaw(server.RawHandle(http.MethodHead, "/ready", headOnlyJSON()))
	mustRaw(server.RawHandle(http.MethodGet, "/ready/", rawJSON(map[string]string{"status": "ready"})))
	mustRaw(server.RawHandle(http.MethodGet, "/routes/static", rawJSON(map[string]string{"route": "static"})))
	// 静态段优先于参数段:/routes/users/new 必须在 /routes/users/{id} 之前注册。
	// A static segment beats a param segment: /routes/users/new must be
	// registered before /routes/users/{id}.
	mustRaw(server.RawHandle(http.MethodGet, "/routes/users/new", rawJSON(map[string]string{"id": "new", "route": "static"})))

	mustRaw(server.RawHandle(http.MethodGet, "/routes/users/{id}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		writeJSON(resp, map[string]string{"id": req.Params.Get("id")})
		return nil
	}))
	mustRaw(server.RawHandle(http.MethodGet, "/routes/pairs/{left}/{right}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		writeJSON(resp, map[string]string{"left": req.Params.Get("left"), "right": req.Params.Get("right")})
		return nil
	}))
	mustRaw(server.RawHandle(http.MethodGet, "/routes/files/{path...}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		writeJSON(resp, map[string]string{"path": strings.TrimPrefix(req.Params.Get("path"), "/")})
		return nil
	}))
	group := server.Group("/routes/groups/v1")
	mustRaw(group.RawHandle(http.MethodGet, "/items/{id}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		writeJSON(resp, map[string]string{"id": req.Params.Get("id")})
		return nil
	}))
	mustRaw(server.RawHandle(http.MethodGet, "/routes/unicode/{value}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		writeJSON(resp, map[string]string{"value": req.Params.Get("value")})
		return nil
	}))

	methodHandler := rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	mustRaw(server.RawHandle(http.MethodPost, "/routes/method", methodHandler))
	mustRaw(server.RawHandle(http.MethodDelete, "/routes/method", methodHandler))

	// catch-all 捕获 %2F 编码的斜杠段(ghttp 按解码后 URL.Path 匹配,编码斜杠跨段)。
	// The catch-all captures %2F-encoded slash segments (ghttp matches on the
	// decoded URL.Path, where an encoded slash spans segments).
	mustRaw(server.RawHandle(http.MethodGet, "/routes/raw-path/{value...}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		r := req.Request
		writeJSON(resp, map[string]string{
			"value":        strings.TrimPrefix(req.Params.Get("value"), "/"),
			"path_value":   r.PathValue("value"),
			"path":         r.URL.Path,
			"raw_path":     r.URL.RawPath,
			"escaped_path": r.URL.EscapedPath(),
			"raw_query":    r.URL.RawQuery,
		})
		return nil
	}))

	mustRaw(server.RawHandle(http.MethodGet, "/__test/metrics", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, metrics.Snapshot())
	}))))
	mustRaw(server.RawHandle(http.MethodPost, "/__test/reset", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		state.reset()
		metrics.Reset()
		faults.Reset()
		w.WriteHeader(http.StatusNoContent)
	}))))
}

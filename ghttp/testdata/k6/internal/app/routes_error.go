package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/sofiworker/gk/gerr"
	"github.com/sofiworker/gk/ghttp"
)

func registerErrors(server *ghttp.Server, cfg Config) {
	secret := cfg.Secret
	if secret == "" {
		secret = testDefaultSecret
	}

	mustRaw(server.RawHandle(http.MethodGet, "/errors/ordinary", errorAdapterHandler(func() error {
		return fmt.Errorf("ordinary-internal: %s", secret)
	})))
	mustRaw(server.RawHandle(http.MethodGet, "/errors/wrapped", errorAdapterHandler(func() error {
		return fmt.Errorf("wrapped %s: %w", secret, errConflictSentinel)
	})))
	mustRaw(server.RawHandle(http.MethodGet, "/errors/joined", errorAdapterHandler(func() error {
		return errors.Join(
			fmt.Errorf("joined %s: %w", secret, errConflictSentinel),
			fmt.Errorf("joined %s: %w", secret, errUnavailableSentinel),
		)
	})))
	mustRaw(server.RawHandle(http.MethodGet, "/errors/gerr", errorAdapterHandler(func() error {
		return gerr.New("gerr "+secret, gerr.WithKind(gerr.KindUnavailable))
	})))
	mustRaw(server.RawHandle(http.MethodGet, "/errors/validation", errorAdapterHandler(func() error {
		return appHTTPError{code: http.StatusUnprocessableEntity, message: http.StatusText(http.StatusUnprocessableEntity)}
	})))
	mustRaw(server.RawHandle(http.MethodGet, "/errors/status/{code}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		status, err := strconv.Atoi(req.Params.Get("code"))
		if err != nil || !allowedErrorStatus(status) {
			writePublicError(resp, http.StatusNotFound)
			return nil
		}
		writePublicError(resp, status)
		return nil
	}))

	// /problem/{kind} 返回 RFC 7807 problem+json。
	// /problem/{kind} returns an RFC 7807 problem+json body.
	mustRaw(server.RawHandle(http.MethodGet, "/problem/{kind}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		kind := req.Params.Get("kind")
		status := http.StatusBadRequest
		if kind == "not-found" {
			status = http.StatusNotFound
		}
		writeProblemJSON(resp, req.Request, status, kind)
		return nil
	}))

	authGroup := server.Group("/auth/user", authMiddleware(secret, false))
	mustRaw(authGroup.RawHandle(http.MethodGet, "", authHandler("user")))
	adminGroup := server.Group("/auth/admin", authMiddleware(secret, true))
	mustRaw(adminGroup.RawHandle(http.MethodGet, "", authHandler("admin")))

	middlewareGroup := server.Group("/middleware", traceMiddleware("group", "X-Group"))
	orderGroup := middlewareGroup.Group("/order", traceMiddleware("route", "X-Route"))
	mustRaw(orderGroup.RawHandle(http.MethodGet, "", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceFromRequest(r).Record("handler")
		writeJSON(w, map[string]string{"status": "ok"})
	}))))
}

// writeProblemJSON 写 RFC 7807 problem+json 错误体。
// writeProblemJSON writes an RFC 7807 problem+json error body.
func writeProblemJSON(w http.ResponseWriter, r *http.Request, status int, kind string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":     "about:blank",
		"title":    http.StatusText(status),
		"status":   status,
		"detail":   "problem " + kind,
		"instance": r.URL.Path,
	})
}

func errorAdapterHandler(build func() error) ghttp.RawHandlerFunc {
	return rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applicationErrorAdapter(w, r, build())
	}))
}

func applicationErrorAdapter(w http.ResponseWriter, r *http.Request, err error) {
	if counter, ok := r.Context().Value(requestCounterContextKey{}).(*requestCounter); ok {
		counter.Increment()
	}
	writePublicError(w, classifyError(err))
}

func allowedErrorStatus(status int) bool {
	switch status {
	case 400, 401, 403, 404, 405, 409, 412, 413, 415, 422, 429, 500, 503, 504:
		return true
	default:
		return false
	}
}

// authMiddleware 验证 Bearer 令牌,失败时返回 401/403 并经错误链写错误体。
// authMiddleware validates the Bearer token, returning 401/403 via the error
// chain on failure.
func authMiddleware(secret string, adminOnly bool) ghttp.Middleware {
	return func(next ghttp.Handler) ghttp.Handler {
		return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
			value := strings.TrimSpace(req.Header.Get("Authorization"))
			if !strings.HasPrefix(value, "Bearer ") {
				writeProblemError(resp, http.StatusUnauthorized, "missing bearer token")
				return nil
			}
			role := strings.TrimPrefix(value, "Bearer ")
			if role != "user:"+secret && role != "admin:"+secret {
				writeProblemError(resp, http.StatusUnauthorized, "invalid token")
				return nil
			}
			if adminOnly && role != "admin:"+secret {
				writeProblemError(resp, http.StatusForbidden, "forbidden role")
				return nil
			}
			return next(ctx, req, resp)
		}
	}
}

// authHandler 构造带角色的认证成功处理器。
// authHandler builds the authenticated handler for a role.
func authHandler(role string) ghttp.RawHandlerFunc {
	return rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Auth-Handler", "executed")
		writeJSON(w, map[string]string{"role": role})
	}))
}

// globalTraceMiddleware 仅对 /middleware/order 施加全局 trace,验证全局-分组-路由三层顺序。
// globalTraceMiddleware applies a global trace only to /middleware/order to
// verify the global-group-route layering order.
func globalTraceMiddleware(next ghttp.Handler) ghttp.Handler {
	return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		if req.URL.Path != "/middleware/order" {
			return next(ctx, req, resp)
		}
		return traceMiddleware("global", "X-Global")(next)(ctx, req, resp)
	}
}

func traceMiddleware(name, vary string) ghttp.Middleware {
	return func(next ghttp.Handler) ghttp.Handler {
		return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
			traceFromRequest(req.Request).Record(name + "-before")
			resp.Header().Add("Vary", vary)
			resp.Header().Set("X-Trace-Owner", name)
			err := next(ctx, req, resp)
			traceFromRequest(req.Request).Record(name + "-after")
			return err
		}
	}
}

type requestCounterContextKey struct{}

func withRequestCounter(request *http.Request, counter *requestCounter) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), requestCounterContextKey{}, counter))
}

type traceSinkContextKey struct{}

type traceSink struct {
	mu    sync.Mutex
	steps []string
}

func newTraceSink() *traceSink { return &traceSink{} }
func (s *traceSink) Record(step string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}
func (s *traceSink) Snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}
func withTraceSink(request *http.Request, sink *traceSink) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), traceSinkContextKey{}, sink))
}
func traceFromRequest(request *http.Request) *traceSink {
	sink, _ := request.Context().Value(traceSinkContextKey{}).(*traceSink)
	return sink
}

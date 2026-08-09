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
	"sync/atomic"

	"github.com/sofiworker/gk/gerr"
	"github.com/sofiworker/gk/ghttp"
)

var (
	errConflictSentinel    = errors.New("conflict-sentinel")
	errUnavailableSentinel = errors.New("unavailable-sentinel")
)

func registerErrors(server *ghttp.Server, cfg Config) {
	secret := cfg.Secret
	if secret == "" {
		secret = testDefaultSecret
	}

	ghttp.Route[struct{}, struct{}](server).GET("/errors/ordinary").ToHTTP(errorAdapterHandler(func() error {
		return fmt.Errorf("ordinary-internal: %s", secret)
	}))
	ghttp.Route[struct{}, struct{}](server).GET("/errors/wrapped").ToHTTP(errorAdapterHandler(func() error {
		return fmt.Errorf("wrapped %s: %w", secret, errConflictSentinel)
	}))
	ghttp.Route[struct{}, struct{}](server).GET("/errors/joined").ToHTTP(errorAdapterHandler(func() error {
		return errors.Join(
			fmt.Errorf("joined %s: %w", secret, errConflictSentinel),
			fmt.Errorf("joined %s: %w", secret, errUnavailableSentinel),
		)
	}))
	ghttp.Route[struct{}, struct{}](server).GET("/errors/gerr").ToHTTP(errorAdapterHandler(func() error {
		return gerr.New("gerr "+secret, gerr.WithKind(gerr.KindUnavailable))
	}))
	ghttp.Route[struct{}, struct{}](server).GET("/errors/validation").ToHTTP(errorAdapterHandler(func() error {
		return ghttp.Err(http.StatusUnprocessableEntity, http.StatusText(http.StatusUnprocessableEntity), ghttp.WithCause(fmt.Errorf("validation %s", secret)))
	}))
	ghttp.Route[ghttp.Params, struct{}](server).GET("/errors/status/{code}").ToHTTPFunc(func(w http.ResponseWriter, r *http.Request, params ghttp.Params) error {
		status, err := strconv.Atoi(params.Path("code"))
		if err != nil || !allowedErrorStatus(status) {
			applicationErrorAdapter(w, r, ghttp.NotFound(http.StatusText(http.StatusNotFound)))
			return nil
		}
		applicationErrorAdapter(w, r, ghttp.Err(status, http.StatusText(status), ghttp.WithCause(fmt.Errorf("controlled %s", secret))))
		return nil
	})
	ghttp.Route[ghttp.Params, struct{}](server).GET("/problem/{kind}").ProblemDetails().To(func(_ context.Context, params ghttp.Params) (struct{}, error) {
		if params.Path("kind") != "not-found" {
			return struct{}{}, ghttp.BadRequest(http.StatusText(http.StatusBadRequest))
		}
		return struct{}{}, ghttp.NotFound(http.StatusText(http.StatusNotFound))
	})

	authGroup := server.Group("/auth")
	ghttp.Route[struct{}, struct{}](authGroup).GET("/user").Use(authMiddleware(secret, false)).ToHTTP(authHandler("user"))
	ghttp.Route[struct{}, struct{}](authGroup).GET("/admin").Use(authMiddleware(secret, true)).ToHTTP(authHandler("admin"))

	middlewareGroup := server.Group("/middleware", traceMiddleware("group", "X-Group"))
	ghttp.Route[struct{}, struct{}](middlewareGroup).GET("/order").Use(traceMiddleware("route", "X-Route")).ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceFromRequest(r).Record("handler")
		writeJSON(w, map[string]string{"status": "ok"})
	}))
}

func errorAdapterHandler(build func() error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applicationErrorAdapter(w, r, build())
	})
}

func applicationErrorAdapter(w http.ResponseWriter, r *http.Request, err error) {
	if counter, ok := r.Context().Value(requestCounterContextKey{}).(*requestCounter); ok {
		counter.Increment()
	}
	writePublicError(w, classifyError(err))
}

func classifyError(err error) int {
	if errors.Is(err, errUnavailableSentinel) {
		return http.StatusServiceUnavailable
	}
	if errors.Is(err, errConflictSentinel) {
		return http.StatusConflict
	}
	if converted, ok := ghttp.FromGerr(err); ok {
		return converted.Code
	}
	if explicit := ghttp.AsError(err); explicit != nil && explicit.Code != 0 {
		return explicit.Code
	}
	return http.StatusInternalServerError
}

func allowedErrorStatus(status int) bool {
	switch status {
	case 400, 401, 403, 404, 405, 409, 412, 413, 415, 422, 429, 500, 503, 504:
		return true
	default:
		return false
	}
}

func authMiddleware(secret string, adminOnly bool) ghttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			value := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(value, "Bearer ") {
				writePublicError(w, http.StatusUnauthorized)
				return
			}
			role := strings.TrimPrefix(value, "Bearer ")
			if role != "user:"+secret && role != "admin:"+secret {
				writePublicError(w, http.StatusUnauthorized)
				return
			}
			if adminOnly && role != "admin:"+secret {
				writePublicError(w, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func authHandler(role string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Auth-Handler", "executed")
		writeJSON(w, map[string]string{"role": role})
	})
}

func writePublicError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ghttp.HTTPError{Code: status, Message: http.StatusText(status)})
}

func globalTraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/middleware/order" {
			next.ServeHTTP(w, r)
			return
		}
		traceMiddleware("global", "X-Global")(next).ServeHTTP(w, r)
	})
}

func traceMiddleware(name, vary string) ghttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceFromRequest(r).Record(name + "-before")
			w.Header().Add("Vary", vary)
			w.Header().Set("X-Trace-Owner", name)
			next.ServeHTTP(w, r)
			traceFromRequest(r).Record(name + "-after")
		})
	}
}

type requestCounter struct{ value atomic.Uint64 }

func newRequestCounter() *requestCounter { return &requestCounter{} }
func (c *requestCounter) Increment()     { c.value.Add(1) }
func (c *requestCounter) Value() uint64  { return c.value.Load() }

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

const testDefaultSecret = "known-k6-secret"

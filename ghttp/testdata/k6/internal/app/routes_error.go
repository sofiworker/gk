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

	server.MustMount(ghttp.RawOperation(http.MethodGet, "/errors/ordinary", errorAdapterHandler(func() error {
		return fmt.Errorf("ordinary-internal: %s", secret)
	})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/errors/wrapped", errorAdapterHandler(func() error {
		return fmt.Errorf("wrapped %s: %w", secret, errConflictSentinel)
	})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/errors/joined", errorAdapterHandler(func() error {
		return errors.Join(
			fmt.Errorf("joined %s: %w", secret, errConflictSentinel),
			fmt.Errorf("joined %s: %w", secret, errUnavailableSentinel),
		)
	})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/errors/gerr", errorAdapterHandler(func() error {
		return gerr.New("gerr "+secret, gerr.WithKind(gerr.KindUnavailable))
	})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/errors/validation", errorAdapterHandler(func() error {
		return ghttp.Err(http.StatusUnprocessableEntity, http.StatusText(http.StatusUnprocessableEntity), ghttp.WithCause(fmt.Errorf("validation %s", secret)))
	})))
	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/errors/status/{code}"), ghttp.PathString("code"), func(w http.ResponseWriter, r *http.Request, code string) error {
		status, err := strconv.Atoi(code)
		if err != nil || !allowedErrorStatus(status) {
			applicationErrorAdapter(w, r, ghttp.NotFound(http.StatusText(http.StatusNotFound)))
			return nil
		}
		applicationErrorAdapter(w, r, ghttp.Err(status, http.StatusText(status), ghttp.WithCause(fmt.Errorf("controlled %s", secret))))
		return nil
	}))
	server.MustMount(ghttp.Handle(ghttp.Get("/problem/{kind}"), ghttp.PathString("kind"), ghttp.JSONOutput[ghttp.EmptyInput](), func(_ context.Context, kind string) (ghttp.EmptyInput, error) {
		if kind != "not-found" {
			return ghttp.EmptyInput{}, ghttp.BadRequest(http.StatusText(http.StatusBadRequest))
		}
		return ghttp.EmptyInput{}, ghttp.NotFound(http.StatusText(http.StatusNotFound))
	}).WithProblemDetails())

	authGroup := server.Group("/auth")
	authGroup.MustMount(ghttp.RawOperation(http.MethodGet, "/user", authHandler("user")).WithMiddleware(authMiddleware(secret, false)))
	authGroup.MustMount(ghttp.RawOperation(http.MethodGet, "/admin", authHandler("admin")).WithMiddleware(authMiddleware(secret, true)))

	middlewareGroup := server.Group("/middleware", traceMiddleware("group", "X-Group"))
	middlewareGroup.MustMount(ghttp.RawOperation(http.MethodGet, "/order", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceFromRequest(r).Record("handler")
		writeJSON(w, map[string]string{"status": "ok"})
	})).WithMiddleware(traceMiddleware("route", "X-Route")))

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

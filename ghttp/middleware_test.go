package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMiddlewareOrder(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	var order []string

	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "a_before")
			next.ServeHTTP(w, r)
			order = append(order, "a_after")
		})
	})

	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "b_before")
			next.ServeHTTP(w, r)
			order = append(order, "b_after")
		})
	})

	var handlerCalled bool
	Route[struct{ Body struct{} }, struct{ Body struct{} }](app).GET("/test").To(func(ctx context.Context, req struct{ Body struct{} }) (struct{ Body struct{} }, error) {
		order = append(order, "handler")
		handlerCalled = true
		return struct{ Body struct{} }{}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	app.ServeHTTP(w, r)

	if !handlerCalled {
		t.Fatal("handler was not called")
	}
	if len(order) != 5 {
		t.Fatalf("expected 5 order entries, got %d: %v", len(order), order)
	}
}

func TestMiddlewareServerGroupAndRoute(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var calls []string

	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "server-before")
			next.ServeHTTP(w, r)
			calls = append(calls, "server-after")
		})
	})

	group := app.Group("/api").Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "group-before")
			next.ServeHTTP(w, r)
			calls = append(calls, "group-after")
		})
	})

	Route[struct{ Body struct{} }, struct{ Body struct{} }](group).
		GET("/test").
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "route-before")
				next.ServeHTTP(w, r)
				calls = append(calls, "route-after")
			})
		}).
		To(func(ctx context.Context, req struct{ Body struct{} }) (struct{ Body struct{} }, error) {
			calls = append(calls, "handler")
			return struct{ Body struct{} }{}, nil
		})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	app.ServeHTTP(w, r)

	want := []string{
		"server-before",
		"group-before",
		"route-before",
		"handler",
		"route-after",
		"group-after",
		"server-after",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
	}
}

func TestMiddlewareWrapStandaloneHandler(t *testing.T) {
	var calls []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, name+"-before")
				next.ServeHTTP(w, r)
				calls = append(calls, name+"-after")
			})
		}
	}

	handler := WrapFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "handler")
		w.WriteHeader(http.StatusAccepted)
	}, Chain(mw("a"), mw("b")))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/standalone", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	want := []string{"a-before", "b-before", "handler", "b-after", "a-after"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
	}
}

func TestRequestLoggerWriterPreservesFlusherWhenSupported(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := newLoggingResponseWriter(rec)

	flusher, ok := rw.(http.Flusher)
	if !ok {
		t.Fatal("expected flusher to be preserved")
	}

	flusher.Flush()

	if !rec.Flushed {
		t.Fatal("expected underlying writer to be flushed")
	}
	if rw.Status() != http.StatusOK {
		t.Fatalf("status = %d, want %d", rw.Status(), http.StatusOK)
	}
}

func TestRequestLoggerWriterDoesNotInventFlusher(t *testing.T) {
	rw := newLoggingResponseWriter(simpleResponseWriter{header: make(http.Header)})
	if _, ok := rw.(http.Flusher); ok {
		t.Fatal("expected wrapper not to implement flusher")
	}
}

type simpleResponseWriter struct {
	header http.Header
}

func (w simpleResponseWriter) Header() http.Header {
	return w.header
}

func (w simpleResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w simpleResponseWriter) WriteHeader(int) {}

func TestBuiltinMiddlewareRequestID(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(RequestID())

	Route[struct{ Body struct{} }, struct{ Body struct{} }](app).GET("/test").To(func(ctx context.Context, req struct{ Body struct{} }) (struct{ Body struct{} }, error) {
		return struct{ Body struct{} }{}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	app.ServeHTTP(w, r)

	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID header")
	}
}

func TestBuiltinMiddlewareRequestIDInjectsContext(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(RequestID())

	var got string
	Route[struct{}, struct{}](app).GET("/test").To(func(ctx context.Context, req struct{}) (struct{}, error) {
		got = GetRequestID(ctx)
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-ID", "req-1")
	app.ServeHTTP(w, r)

	if got != "req-1" {
		t.Fatalf("request id from context = %q, want req-1", got)
	}
}

func TestBuiltinMiddlewareCORSUsesConfiguredHeaders(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(CORS(CORSConfig{
		AllowOrigins: []string{"https://example.test"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
		AllowHeaders: []string{"Content-Type", "X-Request-ID"},
		MaxAge:       60,
	}))

	Route[struct{}, struct{}](app).GET("/cors").To(func(ctx context.Context, req struct{}) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cors", nil)
	r.Header.Set("Origin", "https://example.test")
	app.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.test" {
		t.Fatalf("allow origin = %q, want https://example.test", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Fatalf("allow methods = %q, want GET, POST", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Request-ID" {
		t.Fatalf("allow headers = %q, want Content-Type, X-Request-ID", got)
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "60" {
		t.Fatalf("max age = %q, want 60", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestBuiltinMiddlewareCORSDoesNotEmitAllowHeadersForRejectedOrigin(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(CORS(CORSConfig{
		AllowOrigins: []string{"https://example.test"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
		AllowHeaders: []string{"Content-Type"},
	}))

	Route[struct{}, struct{}](app).GET("/cors").To(func(ctx context.Context, req struct{}) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cors", nil)
	r.Header.Set("Origin", "https://evil.test")
	app.ServeHTTP(w, r)

	for _, header := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
		"Access-Control-Allow-Credentials",
	} {
		if got := w.Header().Get(header); got != "" {
			t.Fatalf("%s = %q, want empty for rejected origin", header, got)
		}
	}
}

func TestBuiltinMiddlewareCORSCredentialsRejectsWildcardOrigin(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(CORS(CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{http.MethodGet},
		AllowCredentials: true,
	}))

	Route[struct{}, struct{}](app).GET("/cors").To(func(ctx context.Context, req struct{}) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cors", nil)
	r.Header.Set("Origin", "https://example.test")
	app.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for credentialed wildcard", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want empty for credentialed wildcard", got)
	}
}

func TestBuiltinMiddlewareRecovery(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Recoverer())

	Route[struct{ Body struct{} }, struct{ Body struct{} }](app).GET("/panic").To(func(ctx context.Context, req struct{ Body struct{} }) (struct{ Body struct{} }, error) {
		panic("test panic")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/panic", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after panic, got %d", w.Code)
	}
}

func TestBuiltinMiddlewareRecoveryUsesStructuredGenericError(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Recoverer())

	Route[struct{}, struct{}](app).GET("/panic").To(func(ctx context.Context, req struct{}) (struct{}, error) {
		panic("secret panic detail")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/panic", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := w.Header().Get("Content-Type"); got != MIMEJSON {
		t.Fatalf("Content-Type = %q, want %s; body = %s", got, MIMEJSON, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret panic detail") {
		t.Fatalf("panic detail leaked in response body: %s", w.Body.String())
	}
	var body HTTPError
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal recovery body failed: %v; body = %s", err, w.Body.String())
	}
	if body.Code != http.StatusInternalServerError || body.Message != "Internal Server Error" {
		t.Fatalf("recovery body = %#v, want generic 500", body)
	}
}

func TestBuiltinMiddlewareTimeoutWritesGatewayTimeout(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Timeout(5 * time.Millisecond))

	Route[struct{}, struct{}](app).GET("/slow").To(func(ctx context.Context, req struct{}) (struct{}, error) {
		time.Sleep(30 * time.Millisecond)
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/slow", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusGatewayTimeout, w.Body.String())
	}
}

func TestBuiltinMiddlewareRecovererCatchesPanicInsideTimeout(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Recoverer())
	app.Use(Timeout(time.Second))

	Route[struct{}, struct{}](app).GET("/panic").To(func(ctx context.Context, req struct{}) (struct{}, error) {
		panic("panic inside timeout")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/panic", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestGroupUseAfterRouteRegistrationApplies(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	group := app.Group("/api")

	Route[struct{}, struct{}](group).GET("/test").To(func(ctx context.Context, req struct{}) (struct{}, error) {
		return struct{}{}, nil
	})

	group.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Group", "applied")
			next.ServeHTTP(w, r)
		})
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	app.ServeHTTP(w, r)

	if got := w.Header().Get("X-Group"); got != "applied" {
		t.Fatalf("X-Group = %q, want applied", got)
	}
}

func TestMiddlewareSeesMatchedPathParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var got string
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = app.MatchedParams(r).Path("id")
			next.ServeHTTP(w, r)
		})
	})
	Route[Params, struct {
		ID string `json:"id"`
	}](app).GET("/users/{id}").To(func(context.Context, Params) (struct {
		ID string `json:"id"`
	}, error) {
		return struct {
			ID string `json:"id"`
		}{ID: "ok"}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if got != "42" {
		t.Fatalf("matched id = %q, want 42", got)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

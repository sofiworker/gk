package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuiltinMiddlewareTimeoutDrainsTailHandler(t *testing.T) {
	var finished atomic.Bool
	app := New(WithProduces(MIMEJSON))
	app.Use(Timeout(10 * time.Millisecond))
	app.MustMount(RawOperation(http.MethodGet, "/slow", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer finished.Store(true)
		time.Sleep(100 * time.Millisecond)
	})))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
	if !finished.Load() {
		t.Fatal("handler tail was not drained before ServeHTTP returned")
	}
}

func TestMiddlewareOrder(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	var order []string

	app.Use(func(c *Ctx) {
		order = append(order, "a_before")
		c.Next()
		order = append(order, "a_after")
	})

	app.Use(func(c *Ctx) {
		order = append(order, "b_before")
		c.Next()
		order = append(order, "b_after")
	})

	var handlerCalled bool
	app.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		order = append(order, "handler")
		handlerCalled = true
		return EmptyInput{}, nil
	}))

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

	app.Use(func(c *Ctx) {
		calls = append(calls, "server-before")
		c.Next()
		calls = append(calls, "server-after")
	})

	group := app.Group("/api").Use(func(c *Ctx) {
		calls = append(calls, "group-before")
		c.Next()
		calls = append(calls, "group-after")
	})

	group.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		calls = append(calls, "handler")
		return EmptyInput{}, nil
	}).WithMiddleware(func(c *Ctx) {
		calls = append(calls, "route-before")
		c.Next()
		calls = append(calls, "route-after")
	}))

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
		return func(c *Ctx) {
			calls = append(calls, name+"-before")
			c.Next()
			calls = append(calls, name+"-after")
		}
	}

	app := New(WithProduces(MIMEJSON))
	app.Use(mw("a"), mw("b"))
	app.MustMount(RawOperation(http.MethodGet, "/standalone", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "handler")
		w.WriteHeader(http.StatusAccepted)
	})))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/standalone", nil)
	app.ServeHTTP(w, r)

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

	app.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

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
	app.Use(func(c *Ctx) {
		got = GetRequestID(c)
		c.Next()
	})

	app.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-ID", "req-1")
	app.ServeHTTP(w, r)

	if got != "req-1" {
		t.Fatalf("request id from context = %q, want req-1", got)
	}
}

func TestBuiltinMiddlewareRequestIDRejectsOversizedIncomingValue(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(RequestID())

	app.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

	long := strings.Repeat("x", 8192)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("X-Request-ID", long)
	app.ServeHTTP(w, r)

	got := w.Header().Get("X-Request-ID")
	if got == long {
		t.Fatal("oversized incoming request id was echoed")
	}
	if len(got) > DefaultMaxRequestIDLength {
		t.Fatalf("response request id length = %d, want <= %d", len(got), DefaultMaxRequestIDLength)
	}
}

func TestBuiltinMiddlewareRequestIDMaxLengthOption(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(RequestID(WithRequestIDMaxLength(8)))

	app.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

	for _, tc := range []struct {
		name string
		id   string
		want string
	}{
		{name: "short echoed", id: "12345678", want: "12345678"},
		{name: "long replaced", id: "123456789", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/test", nil)
			r.Header.Set("X-Request-ID", tc.id)
			app.ServeHTTP(w, r)
			got := w.Header().Get("X-Request-ID")
			if tc.want == "" {
				if got == tc.id {
					t.Fatalf("request id %q was echoed, want replacement", tc.id)
				}
				if len(got) != 32 {
					t.Fatalf("replacement length = %d, want 32", len(got))
				}
				return
			}
			if got != tc.want {
				t.Fatalf("request id = %q, want %q", got, tc.want)
			}
		})
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

	app.MustMount(Handle(Get("/cors"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

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

func TestBuiltinMiddlewareCORSPassesThroughNonPreflightOptions(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(CORS(CORSConfig{
		AllowOrigins: []string{"https://example.test"},
		AllowMethods: []string{http.MethodGet},
	}))

	handlerCalled := 0
	app.MustMount(Handle(Options("/custom"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		handlerCalled++
		return EmptyInput{}, nil
	}))
	app.MustMount(Handle(Get("/custom"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		handlerCalled++
		return EmptyInput{}, nil
	}))

	tests := []struct {
		name         string
		origin       string
		request      string
		preflight    bool
		wantHandler  bool
		wantStatus   int
		wantCORSHead bool
	}{
		{name: "options without origin", request: http.MethodOptions, wantHandler: true, wantStatus: http.StatusOK},
		{name: "options with origin but no preflight", origin: "https://example.test", request: http.MethodOptions, wantHandler: true, wantStatus: http.StatusOK, wantCORSHead: true},
		{name: "preflight", origin: "https://example.test", request: http.MethodOptions, preflight: true, wantHandler: false, wantStatus: http.StatusNoContent, wantCORSHead: true},
		{name: "get with origin", origin: "https://example.test", request: http.MethodGet, wantHandler: true, wantStatus: http.StatusOK, wantCORSHead: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handlerCalled = 0
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tc.request, "/custom", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.preflight {
				r.Header.Set("Access-Control-Request-Method", http.MethodGet)
			}
			app.ServeHTTP(w, r)
			if (handlerCalled > 0) != tc.wantHandler {
				t.Fatalf("handler called = %v, want %v", handlerCalled > 0, tc.wantHandler)
			}
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			got := w.Header().Get("Access-Control-Allow-Origin")
			if (got != "") != tc.wantCORSHead {
				t.Fatalf("cors origin header = %q, want present=%v", got, tc.wantCORSHead)
			}
		})
	}
}

func TestBuiltinMiddlewareCORSDoesNotEmitAllowHeadersForRejectedOrigin(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(CORS(CORSConfig{
		AllowOrigins: []string{"https://example.test"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
		AllowHeaders: []string{"Content-Type"},
	}))

	app.MustMount(Handle(Get("/cors"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

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

	app.MustMount(Handle(Get("/cors"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

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

	app.MustMount(Handle(Get("/panic"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		panic("test panic")
	}))

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

	app.MustMount(Handle(Get("/panic"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		panic("secret panic detail")
	}))

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

	app.MustMount(Handle(Get("/slow"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		time.Sleep(30 * time.Millisecond)
		return EmptyInput{}, nil
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/slow", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusGatewayTimeout, w.Body.String())
	}
}

func TestBuiltinMiddlewareTimeoutStopsLateWrites(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Timeout(10 * time.Millisecond))

	done := make(chan struct{})
	app.MustMount(HandleHTTP(Get("/slow"), NoInput(), func(w http.ResponseWriter, r *http.Request, _ EmptyInput) error {
		defer close(done)
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("late body"))
		return nil
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler goroutine did not finish")
	}
	if strings.Contains(w.Body.String(), "late body") {
		t.Fatalf("late handler write leaked into response: %s", w.Body.String())
	}
}

func TestBuiltinMiddlewareTimeoutCancelsContext(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Timeout(10 * time.Millisecond))

	canceled := make(chan struct{})
	app.MustMount(HandleHTTP(Get("/slow"), NoInput(), func(w http.ResponseWriter, r *http.Request, _ EmptyInput) error {
		<-r.Context().Done()
		close(canceled)
		return nil
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("request context was not canceled after timeout")
	}
}

func TestBuiltinMiddlewareRecovererCatchesPanicInsideTimeout(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Recoverer())
	app.Use(Timeout(time.Second))

	app.MustMount(Handle(Get("/panic"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		panic("panic inside timeout")
	}))

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

	group.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

	group.Use(func(c *Ctx) {
		c.W.Header().Set("X-Group", "applied")
		c.Next()
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	app.ServeHTTP(w, r)

	if got := w.Header().Get("X-Group"); got != "applied" {
		t.Fatalf("X-Group = %q, want applied", got)
	}
}

func TestGroupMiddlewareSnapshotAtCreation(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	parent := app.Group("/api")
	child := parent.Group("/v1")
	parent.Use(func(c *Ctx) {
		c.W.Header().Set("X-Parent-Late", "applied")
		c.Next()
	})

	child.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Parent-Late"); got != "" {
		t.Fatalf("X-Parent-Late = %q, want empty for middleware added after child creation", got)
	}
}

func TestGroupMiddlewareSnapshotIncludesParentBeforeCreation(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	parent := app.Group("/api")
	parent.Use(func(c *Ctx) {
		c.W.Header().Set("X-Parent-Early", "applied")
		c.Next()
	})
	child := parent.Group("/v1")

	child.MustMount(Handle(Get("/test"), NoInput(), JSONOutput[EmptyInput](), func(ctx context.Context, _ EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Parent-Early"); got != "applied" {
		t.Fatalf("X-Parent-Early = %q, want applied", got)
	}
}

func TestServerUseChainable(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	mw := func(c *Ctx) { c.Next() }
	if got := app.Use(mw); got != app {
		t.Fatal("Server.Use must return the server for chaining")
	}
}

func TestMiddlewareSeesMatchedPathParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var got string
	app.Use(func(c *Ctx) {
		got = app.MatchedParams(c.R).Path("id")
		c.Next()
	})

	app.MustMount(Handle(Get("/users/{id}"), NoInput(), JSONOutput[struct {
		ID string `json:"id"`
	}](), func(_ context.Context, _ EmptyInput) (struct {
		ID string `json:"id"`
	}, error) {
		return struct {
			ID string `json:"id"`
		}{ID: "ok"}, nil
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if got != "42" {
		t.Fatalf("matched id = %q, want 42", got)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

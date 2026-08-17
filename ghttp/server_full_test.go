package ghttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type standardMethodOutput struct {
	Method string `json:"method"`
}

type serverValidatorFunc func(context.Context, interface{}) error

func (f serverValidatorFunc) Validate(ctx context.Context, input interface{}) error {
	return f(ctx, input)
}

func TestServerNewInitializesDefaultsAndOptions(t *testing.T) {
	validator := serverValidatorFunc(func(context.Context, interface{}) error { return nil })
	envelope := func(http.ResponseWriter, *http.Request, int, interface{}, error, string, Codec) {}

	app := New(WithAddress("127.0.0.1:0"), WithValidator(validator), WithEnvelope(envelope), WithProduces(MIMEJSON))

	if app == nil {
		t.Fatal("New returned nil")
	}
	if app.config.address != "127.0.0.1:0" {
		t.Fatalf("address = %q, want %q", app.config.address, "127.0.0.1:0")
	}
	if app.validator == nil {
		t.Fatal("WithValidator should install validator")
	}
	if app.codecMgr == nil {
		t.Fatal("codec manager should be initialized")
	}
	if app.envelope == nil {
		t.Fatal("WithEnvelope should install envelope")
	}
	if _, err := app.OpenAPI(); !errors.Is(err, ErrOpenAPIDisabled) {
		t.Fatalf("OpenAPI error = %v, want ErrOpenAPIDisabled", err)
	}
}

func TestServerRenderHTMLViaBuilder(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(`<h1>{{.Title}}</h1>`), 0o600); err != nil {
		t.Fatalf("write template failed: %v", err)
	}

	app := New(WithRenderer(NewRenderer(tmpDir, ".html", template.FuncMap{}, false)))

	app.MustMount(HTMLViewOperation(http.MethodGet, "/page", http.StatusCreated, "index", map[string]interface{}{"Title": "Hello"}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("<h1>Hello</h1>")) {
		t.Fatalf("body = %q, want rendered html", rec.Body.String())
	}
}

func TestServerMiddlewareChainOrder(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var calls []string

	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "mw1-before")
			next.ServeHTTP(w, r)
			calls = append(calls, "mw1-after")
		})
	})
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "mw2-before")
			next.ServeHTTP(w, r)
			calls = append(calls, "mw2-after")
		})
	})

	app.MustMount(RawOperation(http.MethodGet, "/ok", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "handler")
		w.WriteHeader(http.StatusNoContent)
	})))

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	want := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestServerRegistersAllStandardHTTPMethods(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		register func(*Server, string)
	}{
		{http.MethodGet, "/standard/get", func(s *Server, path string) {
			s.MustMount(Handle(Get(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodGet)))
		}},
		{http.MethodHead, "/standard/head", func(s *Server, path string) {
			s.MustMount(Handle(Head(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodHead)))
		}},
		{http.MethodPost, "/standard/post", func(s *Server, path string) {
			s.MustMount(Handle(Post(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodPost)))
		}},
		{http.MethodPut, "/standard/put", func(s *Server, path string) {
			s.MustMount(Handle(Put(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodPut)))
		}},
		{http.MethodPatch, "/standard/patch", func(s *Server, path string) {
			s.MustMount(Handle(Patch(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodPatch)))
		}},
		{http.MethodDelete, "/standard/delete", func(s *Server, path string) {
			s.MustMount(Handle(Delete(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodDelete)))
		}},
		{http.MethodConnect, "/standard/connect", func(s *Server, path string) {
			s.MustMount(Handle(Connect(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodConnect)))
		}},
		{http.MethodOptions, "/standard/options", func(s *Server, path string) {
			s.MustMount(Handle(Options(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodOptions)))
		}},
		{http.MethodTrace, "/standard/trace", func(s *Server, path string) {
			s.MustMount(Handle(Trace(path), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler(http.MethodTrace)))
		}},
	}

	app := New(WithProduces(MIMEJSON))
	for _, tt := range tests {
		tt.register(app, tt.path)
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d, want %d", tt.method, tt.path, rec.Code, http.StatusOK)
		}
	}
}

func TestOperationAllMethodsRegistersAllStandardHTTPMethods(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(AllMethods(Handle(Get("/any"), StructInput[struct{}](), JSONOutput[standardMethodOutput](), func(ctx context.Context, req struct{}) (standardMethodOutput, error) {
		return standardMethodOutput{Method: ""}, nil
	}))...)

	methods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
	}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/any", nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s /any status = %d, want %d", method, rec.Code, http.StatusOK)
		}
	}
}

func TestOperationMountRequiresMethod(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	assertPanicsIs(t, ErrOperationMethodRequired, func() {
		app.MustMount(Handle(Endpoint("", "/missing-method"), StructInput[struct{}](), JSONOutput[struct{}](), func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, nil
		}))
	})
}

func TestOperationCustomMethodRegistersCustomMethod(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Endpoint("PROPFIND", "/custom"), StructInput[struct{}](), JSONOutput[standardMethodOutput](), methodOutputHandler("PROPFIND")))

	req := httptest.NewRequest("PROPFIND", "/custom", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestOperationCustomMethodRejectsInvalidMethod(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	assertPanicsIs(t, ErrOperationMethodInvalid, func() {
		app.MustMount(Handle(Endpoint("BAD METHOD", "/custom"), StructInput[struct{}](), JSONOutput[struct{}](), func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, nil
		}))
	})
}

func methodOutputHandler(method string) HandlerFunc[struct{}, standardMethodOutput] {
	return func(context.Context, struct{}) (standardMethodOutput, error) {
		return standardMethodOutput{Method: method}, nil
	}
}

func TestServerOpenAPIEndpoint(t *testing.T) {
	type req struct {
		ID string `path:"id"`
	}
	type resp struct {
		Name string `json:"name"`
	}

	app := New(WithOpenAPI("accounts", "2.0.0"), WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/users/{id}"), StructInput[req](), JSONOutput[resp](), func(context.Context, req) (resp, error) {
		return resp{Name: "alice"}, nil
	}).Doc(Summary("get user"),
		OperationID("getUser"),
		Tags("users"),
		Success(Message("user"))))

	httpReq := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid openapi json: %v", err)
	}
	info := doc["info"].(map[string]interface{})
	if info["title"] != "accounts" || info["version"] != "2.0.0" {
		t.Fatalf("info = %#v, want title/version from WithOpenAPI", info)
	}
	paths := doc["paths"].(map[string]interface{})
	if _, ok := paths["/users/{id}"]; !ok {
		t.Fatalf("paths = %#v, want /users/{id}", paths)
	}
}

func TestServerRouteUsesValidator(t *testing.T) {
	wantErr := errors.New("invalid input")
	app := New(WithValidator(serverValidatorFunc(func(ctx context.Context, input interface{}) error {
		return wantErr
	})), WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/validate"), StructInput[struct{}](), JSONOutput[struct{}](), func(context.Context, struct{}) (struct{}, error) {
		t.Fatal("handler should not run after validation error")
		return struct{}{}, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "/validate", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestServerUsesGoPlaygroundValidatorWhenExplicit(t *testing.T) {
	type input struct {
		Body struct {
			Name string `json:"name" validate:"required"`
		}
	}
	type output struct{}

	app := New(WithProduces(MIMEJSON), WithValidator(newDefaultValidator()))
	app.MustMount(Handle(Post("/validate/default"), StructInput[input](), JSONOutput[output](), func(context.Context, input) (output, error) {
		t.Fatal("handler should not run after default validation error")
		return output{}, nil
	}))

	req := httptest.NewRequest(http.MethodPost, "/validate/default", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestServerRunInitializesHTTPServerWithOverrideAddress(t *testing.T) {
	app := New(WithAddress("127.0.0.1:0"))

	err := app.Run("bad-address")
	if err == nil {
		t.Fatal("Run should fail for an invalid address")
	}
	if app.httpServer == nil {
		t.Fatal("Run should initialize httpServer before ListenAndServe")
	}
	if app.httpServer.Addr != "bad-address" {
		t.Fatalf("httpServer.Addr = %q, want override address", app.httpServer.Addr)
	}
	if app.httpServer.Handler == nil {
		t.Fatal("Run should install handler chain")
	}
}

func TestServerRunAppliesHTTPServerOptions(t *testing.T) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	errorLog := log.New(&bytes.Buffer{}, "ghttp-test: ", 0)
	baseContext := func(net.Listener) context.Context {
		return context.Background()
	}
	connContext := func(ctx context.Context, conn net.Conn) context.Context {
		return ctx
	}

	app := New(
		WithReadTimeout(time.Second),
		WithReadHeaderTimeout(2*time.Second),
		WithWriteTimeout(3*time.Second),
		WithIdleTimeout(4*time.Second),
		WithMaxHeaderBytes(1<<19),
		WithTLSConfig(tlsConfig),
		WithBaseContext(baseContext),
		WithConnContext(connContext),
		WithErrorLog(errorLog),
	)

	err := app.Run("bad-address")
	if err == nil {
		t.Fatal("Run should fail for an invalid address")
	}
	if app.httpServer == nil {
		t.Fatal("Run should initialize httpServer")
	}
	if app.httpServer.ReadTimeout != time.Second {
		t.Fatalf("ReadTimeout = %v, want %v", app.httpServer.ReadTimeout, time.Second)
	}
	if app.httpServer.ReadHeaderTimeout != 2*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", app.httpServer.ReadHeaderTimeout, 2*time.Second)
	}
	if app.httpServer.WriteTimeout != 3*time.Second {
		t.Fatalf("WriteTimeout = %v, want %v", app.httpServer.WriteTimeout, 3*time.Second)
	}
	if app.httpServer.IdleTimeout != 4*time.Second {
		t.Fatalf("IdleTimeout = %v, want %v", app.httpServer.IdleTimeout, 4*time.Second)
	}
	if app.httpServer.MaxHeaderBytes != 1<<19 {
		t.Fatalf("MaxHeaderBytes = %d, want %d", app.httpServer.MaxHeaderBytes, 1<<19)
	}
	if app.httpServer.TLSConfig != tlsConfig {
		t.Fatal("TLSConfig was not applied")
	}
	if app.httpServer.BaseContext == nil {
		t.Fatal("BaseContext was not applied")
	}
	if app.httpServer.ConnContext == nil {
		t.Fatal("ConnContext was not applied")
	}
	if app.httpServer.ErrorLog != errorLog {
		t.Fatal("ErrorLog was not applied")
	}
}

func TestServerShutdownWithoutRunAndAfterRunFailure(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown before Run failed: %v", err)
	}

	_ = app.Run("bad-address")
	if app.httpServer == nil {
		t.Fatal("Run should initialize httpServer")
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown after Run failure failed: %v", err)
	}
}

func TestServerShutdownRequiresContext(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	err := app.Shutdown(nil) //nolint:staticcheck // intentionally verifies ErrNilContext

	if !errors.Is(err, ErrNilContext) {
		t.Fatalf("Shutdown(nil) error = %v, want ErrNilContext", err)
	}
}

func TestServerShutdownStopsRunningServerGracefully(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	errCh := make(chan error, 1)

	go func() {
		errCh <- app.Run("127.0.0.1:0")
	}()

	waitForHTTPServer(t, app)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error after shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after Shutdown")
	}
}

func TestServerRunExposesActualAddrForZeroPort(t *testing.T) {
	app := New()
	errCh := make(chan error, 1)

	go func() {
		errCh <- app.Run("127.0.0.1:0")
	}()

	addr := waitForServerAddr(t, app)
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatalf("SplitHostPort failed: %v", err)
	}
	if port == "" || port == "0" {
		t.Fatalf("Addr = %q, want actual listener port", addr.String())
	}

	if err := app.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error after Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after Close")
	}
}

func TestServerServeUsesExistingListener(t *testing.T) {
	app := New()
	app.MustMount(RawOperation(http.MethodGet, "/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Serve(ln)
	}()

	addr := waitForServerAddr(t, app)
	resp, err := http.Get("http://" + addr.String() + "/ping")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if err := app.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve returned error after Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after Close")
	}
}

func TestServerServeRejectsNilListener(t *testing.T) {
	app := New()
	if err := app.Serve(nil); !errors.Is(err, ErrNilListener) {
		t.Fatalf("Serve(nil) error = %v, want ErrNilListener", err)
	}
	if err := app.ServeTLS(nil, "", ""); !errors.Is(err, ErrNilListener) {
		t.Fatalf("ServeTLS(nil) error = %v, want ErrNilListener", err)
	}
}

func TestServerListenAndServeTLSInitializesHTTPServer(t *testing.T) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	app := New(WithTLSConfig(tlsConfig))

	err := app.ListenAndServeTLS("bad-address", "", "")
	if err == nil {
		t.Fatal("ListenAndServeTLS should fail for an invalid address")
	}
	if app.httpServer == nil {
		t.Fatal("ListenAndServeTLS should initialize httpServer")
	}
	if app.httpServer.TLSConfig != tlsConfig {
		t.Fatal("TLSConfig was not applied")
	}
}

func TestServerCloseStopsRunningServerWithoutErrServerClosed(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	errCh := make(chan error, 1)

	go func() {
		errCh <- app.Run("127.0.0.1:0")
	}()

	waitForHTTPServer(t, app)

	if err := app.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error after Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after Close")
	}
}

func waitForHTTPServer(t *testing.T, app *Server) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		ready := app.httpServer != nil
		app.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("server did not initialize httpServer")
}

func waitForServerAddr(t *testing.T, app *Server) net.Addr {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if addr := app.Addr(); addr != nil {
			return addr
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("server did not expose listener address")
	return nil
}

func TestServerGroupStoresPrefixAndMiddlewares(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	mw := func(next http.Handler) http.Handler { return next }

	group := app.Group("/api", mw)

	if group.server != app {
		t.Fatal("group should keep parent server")
	}
	if group.prefix != "/api" {
		t.Fatalf("group.prefix = %q, want /api", group.prefix)
	}
	if len(group.middlewares) != 1 || group.middlewares[0] == nil {
		t.Fatalf("group middlewares = %#v, want one middleware", group.middlewares)
	}
}

func TestGroupRouteRegistersRouteWithPrefixAndMiddleware(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	group := app.Group("/api", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Group", "api")
			next.ServeHTTP(w, r)
		})
	})

	group.MustMount(Handle(Get("/ping"), StructInput[struct{}](), JSONOutput[struct {
		OK bool `json:"ok"`
	}](), func(context.Context, struct{}) (struct {
		OK bool `json:"ok"`
	}, error) {
		return struct {
			OK bool `json:"ok"`
		}{OK: true}, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Group"); got != "api" {
		t.Fatalf("X-Group = %q, want api", got)
	}
}

func TestNestedGroupCombinesPrefixAndMiddlewares(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var calls []string
	v1 := app.Group("/api", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "api-in")
			next.ServeHTTP(w, r)
			calls = append(calls, "api-out")
		})
	})
	users := v1.Group("/users", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "users-in")
			next.ServeHTTP(w, r)
			calls = append(calls, "users-out")
		})
	})

	users.MustMount(RawOperation(http.MethodGet, "/me", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "handler")
		w.WriteHeader(http.StatusNoContent)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/users/me", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	want := []string{"api-in", "users-in", "handler", "users-out", "api-out"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestGroupOperationUsesGroupPath(t *testing.T) {
	type input struct {
		Params `json:"-"`
	}
	type output struct {
		ID string `json:"id"`
	}

	app := New(WithProduces(MIMEJSON))
	group := app.Group("/api")
	group.MustMount(Handle(Get("/users/{id}"), StructInput[input](), JSONOutput[output](), func(ctx context.Context, req input) (output, error) {
		return output{ID: req.Path("id")}, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/users/42", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"id":"42"`) {
		t.Fatalf("response body = %s, want id 42", rec.Body.String())
	}
}

func TestGroupOperationUsesGroupPathInOpenAPI(t *testing.T) {
	type input struct{}
	type output struct{}

	app := New(WithOpenAPI("api", "1.0.0"), WithProduces(MIMEJSON))
	group := app.Group("/api")

	group.MustMount(Handle(Post("/users"), StructInput[input](), JSONOutput[output](), func(context.Context, input) (output, error) {
		return output{}, nil
	}).Doc(Summary("create user")))

	spec := serverOpenAPISpec(t, app)
	var doc map[string]interface{}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("invalid openapi json: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	if _, ok := paths["/api/users"]; !ok {
		t.Fatalf("paths = %#v, want /api/users", paths)
	}
}

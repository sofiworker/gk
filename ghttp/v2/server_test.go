package v2

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	root "github.com/sofiworker/gk/ghttp"
)

func TestServerRoutesAndMiddleware(t *testing.T) {
	var visited []string
	middleware := func(next root.Handler) root.Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			visited = append(visited, "global")
			return next(ctx, req, resp)
		}
	}
	s := NewServer().Use(middleware).With(WithOutput(TextOutput[string]()))
	if err := s.Register(Get("/", func(context.Context, struct{}) (string, error) { return "root", nil })); err != nil {
		t.Fatal(err)
	}
	group := s.Group("/api")
	if err := group.Register(Get("/hello", func(context.Context, struct{}) (string, error) { return "group", nil })); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{{"/", "root"}, {"/api/hello", "group"}} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != tc.want {
			t.Errorf("GET %s: status=%d body=%q", tc.path, rec.Code, rec.Body.String())
		}
	}
	if len(visited) != 2 {
		t.Fatalf("middleware calls = %d, want 2", len(visited))
	}
	if err := s.Register(Get("/", func(context.Context, struct{}) (string, error) { return "duplicate", nil })); !errors.Is(err, root.ErrDuplicateRoute) {
		t.Fatalf("duplicate route error = %v", err)
	}
}

func TestServerErrorHandler(t *testing.T) {
	boom := errors.New("boom")
	var status int
	var observed error
	s := NewServer(WithErrorHandler(func(_ *http.Request, code int, err error) {
		status, observed = code, err
	}))
	if err := s.Register(Get("/broken", func(context.Context, struct{}) (string, error) {
		return "", boom
	})); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/broken", nil))
	if rec.Code != http.StatusInternalServerError || status != rec.Code || !errors.Is(observed, boom) {
		t.Fatalf("response=%d observed=%d err=%v", rec.Code, status, observed)
	}
}

func TestServerLifecycle(t *testing.T) {
	s := NewServer()
	if err := s.RunTLS("", "", ""); !errors.Is(err, ErrTLSConfig) {
		t.Fatalf("RunTLS without certificates = %v", err)
	}
	listener := &blockingListener{accepted: make(chan struct{}), closed: make(chan struct{})}
	if err := s.ServeTLS(listener, "", ""); !errors.Is(err, ErrTLSConfig) {
		t.Fatalf("ServeTLS without certificates = %v", err)
	}
	result := make(chan error, 1)
	go func() { result <- s.Serve(listener) }()
	select {
	case <-listener.accepted:
	case <-time.After(time.Second):
		t.Fatal("Serve did not accept connections")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrServerClosed) {
			t.Fatalf("Serve after Shutdown = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
	if err := s.Run(""); !errors.Is(err, ErrServerNotStartable) {
		t.Fatalf("Run after Shutdown = %v", err)
	}
}

type blockingListener struct {
	accepted   chan struct{}
	closed     chan struct{}
	acceptOnce sync.Once
	closeOnce  sync.Once
}

func (l *blockingListener) Accept() (net.Conn, error) {
	l.acceptOnce.Do(func() { close(l.accepted) })
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *blockingListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestServerHTTPMethods(t *testing.T) {
	s := NewServer()
	if err := s.Register(
		Get("/resource", func(context.Context, struct{}) (string, error) { return "get", nil }, WithOutput(TextOutput[string]())),
		Head("/resource", func(context.Context, struct{}) (string, error) { return "head", nil }, WithOutput(TextOutput[string]())),
		Options("/resource", func(context.Context, struct{}) (string, error) { return "options", nil }, WithOutput(TextOutput[string]())),
	); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		status int
		body   string
	}{
		{http.MethodGet, http.StatusOK, "get"},
		{http.MethodHead, http.StatusOK, ""},
		{http.MethodOptions, http.StatusOK, "options"},
		{http.MethodPost, http.StatusMethodNotAllowed, ""},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(tc.method, "/resource", nil))
		if rec.Code != tc.status || !strings.HasPrefix(rec.Body.String(), tc.body) {
			t.Errorf("%s: status=%d body=%q", tc.method, rec.Code, rec.Body.String())
		}
		if tc.method == http.MethodHead && rec.Body.Len() != 0 {
			t.Errorf("HEAD returned body %q", rec.Body.String())
		}
		if tc.method == http.MethodPost && !strings.Contains(rec.Header().Get("Allow"), http.MethodGet) {
			t.Errorf("405 Allow = %q", rec.Header().Get("Allow"))
		}
	}
}

func TestServerRegisterFailureBoundary(t *testing.T) {
	s := NewServer()
	handler := func(context.Context, struct{}) (string, error) { return "ok", nil }
	if err := s.Register(Get("/existing", handler)); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(Get("/invalid-batch", handler), Get("/invalid", handler, WithBodyLimit(0))); err == nil {
		t.Fatal("expected batch validation failure")
	}
	if err := s.Register(Get("/installed", handler), Get("/existing", handler)); !errors.Is(err, root.ErrDuplicateRoute) {
		t.Fatalf("backend duplicate route error = %v", err)
	}
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/invalid-batch", http.StatusNotFound},
		{"/installed", http.StatusOK},
		{"/existing", http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.code {
			t.Errorf("GET %s: status=%d, want %d", tc.path, rec.Code, tc.code)
		}
	}
}

func TestServerOptions(t *testing.T) {
	s := NewServer(
		WithAddr("127.0.0.1:0"), WithReadTimeout(time.Second),
		WithReadHeaderTimeout(time.Second), WithWriteTimeout(time.Second),
		WithIdleTimeout(time.Second), WithMaxHeaderBytes(4096),
		WithTLSConfig(nil), WithBaseContext(func(net.Listener) context.Context { return context.Background() }),
	)
	if err := s.Register(Post("/echo", func(_ context.Context, in string) (string, error) {
		return in, nil
	}, WithInput(TextInput[string]()), WithOutput(TextOutput[string]()))); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("hi"))
	req.Header.Set("Content-Type", "text/plain")
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "hi" {
		t.Fatalf("response: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

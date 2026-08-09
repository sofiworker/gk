package ghttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sofiworker/gk/gerr"
)

func TestHTTPErrorConstructors(t *testing.T) {
	// Err 带 cause 与空消息回退。
	// Err with cause and empty-message fallback.
	cause := errors.New("db down")
	e := Err(http.StatusBadRequest, "", WithCause(cause))
	if e.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want 400", e.Code)
	}
	if e.Error() != http.StatusText(http.StatusBadRequest) {
		t.Fatalf("Error() = %q, want status text fallback", e.Error())
	}
	if !errors.Is(e, cause) {
		t.Fatal("Unwrap should preserve cause chain")
	}

	// 带消息的 Error()。
	if got := Err(404, "missing").Error(); got != "missing" {
		t.Fatalf("Error() = %q, want message", got)
	}

	// 便捷构造器。
	if BadRequest("bad").Code != 400 || NotFound("nf").Code != 404 ||
		Conflict("c").Code != 409 || InternalError("ie").Code != 500 {
		t.Fatal("convenience constructors return wrong status codes")
	}
}

func TestAsErrorExtractsFromChain(t *testing.T) {
	he := Err(409, "conflict")
	wrapped := fmt.Errorf("wrap: %w", he)

	got := AsError(wrapped)
	if got == nil || got.Code != 409 {
		t.Fatalf("AsError = %+v, want the 409 HTTPError", got)
	}
	if AsError(errors.New("plain")) != nil {
		t.Fatal("AsError should return nil for non-HTTPError")
	}
}

func TestGerrBridge(t *testing.T) {
	ge := gerr.New("user gone", gerr.WithKind(gerr.KindNotFound))

	// GerrStatus 映射。
	if status, ok := GerrStatus(ge); !ok || status != http.StatusNotFound {
		t.Fatalf("GerrStatus = %d,%v want 404,true", status, ok)
	}
	if _, ok := GerrStatus(errors.New("plain")); ok {
		t.Fatal("GerrStatus should return false for plain errors")
	}

	// FromGerr → HTTPError。
	he, ok := FromGerr(ge)
	if !ok || he.Code != http.StatusNotFound {
		t.Fatalf("FromGerr = %+v, want 404", he)
	}

	// ToGerr 反向。
	back, ok := ToGerr(he)
	if !ok || back == nil {
		t.Fatal("ToGerr should convert HTTPError back to gerr")
	}
	// HTTPError 不是 gerr，直接 ToGerr 失败。
	if _, ok := ToGerr(errors.New("plain")); ok {
		t.Fatal("ToGerr should fail on plain errors")
	}
}

func TestJoinPaths(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"/api", "/users", "/api/users"},
		{"/api/", "/users", "/api/users"},
		{"/api", "/", "/api/"},
		{"", "/users", "/users"},
		{"/api", "", "/api"},
	}
	for _, tc := range cases {
		if got := JoinPaths(tc.a, tc.b); got != tc.want {
			t.Fatalf("JoinPaths(%q,%q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestServerRecoversHandlerPanic(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/boom").ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 after panic recovery", w.Code)
	}
}

func TestServerMatchedParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var gotPath string
	Route[ghttpParamsAlias, struct{}](app).GET("/users/{id}").ToHTTPFunc(func(w http.ResponseWriter, r *http.Request, in ghttpParamsAlias) error {
		gotPath = app.MatchedParams(r).Path("id")
		w.WriteHeader(http.StatusNoContent)
		return nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/u42", nil))
	if gotPath != "u42" {
		t.Fatalf("MatchedParams path = %q, want u42", gotPath)
	}
	// nil 请求安全。
	if app.MatchedParams(nil).Path("id") != "" {
		t.Fatal("MatchedParams(nil) should return empty Params")
	}
}

func TestServerNormalizeHTTPError(t *testing.T) {
	// 显式 HTTPError 保留，空 Code 用默认补。
	explicit := normalizeHTTPError(500, Err(0, "custom"))
	if explicit.Code != 500 || explicit.Message != "custom" {
		t.Fatalf("explicit normalize = %+v", explicit)
	}

	// 普通错误 → 默认 500。
	plain := normalizeHTTPError(0, errors.New("boom"))
	if plain.Code != http.StatusInternalServerError {
		t.Fatalf("plain normalize = %+v, want 500", plain)
	}

	// 4xx 保留原始状态文本。
	nf := normalizeHTTPError(404, errors.New("missing"))
	if nf.Code != 404 || nf.Message != http.StatusText(404) {
		t.Fatalf("404 normalize = %+v", nf)
	}
}

func TestServerDispatchError(t *testing.T) {
	var handled *HTTPError
	app := New(
		WithProduces(MIMEJSON),
		WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err *HTTPError) {
			handled = err
			w.WriteHeader(err.Code)
		}),
	)

	// 通过路由返回错误触发 dispatchError。
	Route[struct{}, struct{}](app).GET("/fail").ToNoOutput(func(ctx context.Context, req struct{}) error {
		return Err(http.StatusTeapot, "teapot")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fail", nil))
	if handled == nil || handled.Code != http.StatusTeapot {
		t.Fatalf("dispatchError handled = %+v, want 418", handled)
	}
	if w.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", w.Code)
	}
}

func TestServerServeTLSRejectsNilListener(t *testing.T) {
	app := New()
	if err := app.ServeTLS(nil, "cert", "key"); !errors.Is(err, ErrNilListener) {
		t.Fatalf("ServeTLS(nil) = %v, want ErrNilListener", err)
	}
	if err := app.Serve(nil); !errors.Is(err, ErrNilListener) {
		t.Fatalf("Serve(nil) = %v, want ErrNilListener", err)
	}
}

func TestServerListenAndServeTLSInvalidFiles(t *testing.T) {
	app := New()
	// 证书文件不存在 → 服务启动应返回错误。
	err := app.ListenAndServeTLS("127.0.0.1:0", "missing.crt", "missing.key")
	if err == nil {
		t.Fatal("ListenAndServeTLS with missing cert files should error")
	}
}

func TestServerLifecycleWithoutRun(t *testing.T) {
	app := New()
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown before Run = %v, want nil", err)
	}
	if err := app.Close(); err != nil {
		t.Fatalf("Close before Run = %v, want nil", err)
	}
	if app.Addr() != nil {
		t.Fatal("Addr before Run should be nil")
	}
	// nil context 拒绝。
	if err := app.Shutdown(nil); !errors.Is(err, ErrNilContext) {
		t.Fatalf("Shutdown(nil) = %v, want ErrNilContext", err)
	}
}

func TestServerRunConflictAddr(t *testing.T) {
	// 占用一个端口，再让第二个 server 监听同一端口应失败。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	app := New()
	if err := app.Run(addr); err == nil {
		t.Fatal("Run on a busy address should error")
	}
}

// ghttpParamsAlias 仅在测试内避免重复导入冲突。
// ghttpParamsAlias avoids a duplicate import alias inside the test package.
type ghttpParamsAlias = Params

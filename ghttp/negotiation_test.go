package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestExplicitOutputContractIgnoresAccept 验证显式输出契约不协商:JSONOutput
// 已声明响应格式,Accept 头不改变输出。
// TestExplicitOutputContractIgnoresAccept verifies explicit output contracts
// do not negotiate: JSONOutput already declares the format, so Accept does not
// change the output.
func TestExplicitOutputContractIgnoresAccept(t *testing.T) {
	type pingResp struct {
		Name string `json:"name"`
	}
	app := New(WithProduces(MIMEJSON), WithStrictContentNegotiation())
	app.MustMount(Handle(Get("/ping"), NoInput(), JSONOutput[pingResp](), func(_ context.Context, _ EmptyInput) (pingResp, error) {
		return pingResp{Name: "pong"}, nil
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "text/html")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestStrictContentNegotiationReturns406 验证 CodecOutput(显式协商契约)在严格
// 模式下 Accept 不匹配返回 406。
// TestStrictContentNegotiationReturns406 verifies CodecOutput, the explicit
// negotiation contract, returns 406 when Accept does not match in strict mode.
func TestStrictContentNegotiationReturns406(t *testing.T) {
	type pingResp struct {
		Name string `json:"name"`
	}
	app := New(WithProduces(MIMEJSON), WithStrictContentNegotiation())
	app.MustMount(Handle(Get("/ping"), NoInput(), CodecOutput[pingResp](MIMEJSON), func(_ context.Context, _ EmptyInput) (pingResp, error) {
		return pingResp{Name: "pong"}, nil
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "text/html")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNotAcceptable, w.Body.String())
	}
}

func TestLenientContentNegotiationFallsBack(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithLenientContentNegotiation())
	app.MustMount(Handle(Get("/ping"), NoInput(), CodecOutput[map[string]string](MIMEJSON), func(_ context.Context, _ EmptyInput) (map[string]string, error) {
		return map[string]string{"name": "pong"}, nil
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "text/html")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Content-Type"); got != MIMEJSON {
		t.Fatalf("content type = %q, want %q", got, MIMEJSON)
	}
}

func TestStrictContentTypeReturns415(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/users"), JSONBody[input](), JSONOutput[EmptyInput](), func(_ context.Context, _ input) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("Content-Type", "application/octet-stream")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusUnsupportedMediaType, w.Body.String())
	}
}

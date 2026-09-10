package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// autoHEAD 的响应体丢弃由 net/http 服务层完成(而非框架内包装 writer 吞字节),因此
// 测试必须经 httptest.NewServer 走真实 HTTP 栈:那一层在丢体的同时仍按写入字节计算
// Content-Length、做 Content-Type 嗅探,HEAD 的元数据得以与 GET 一致(RFC 9110 §9.3.2)。
// autoHEAD body dropping is done by the net/http serving layer (not by a
// byte-swallowing writer wrapper inside the framework), so these tests must go
// through httptest.NewServer: that layer drops the body while still deriving
// Content-Length and sniffing Content-Type from the written bytes, keeping HEAD
// metadata identical to GET (RFC 9110 §9.3.2).
func TestAutoHEADFallsBackToGET(t *testing.T) {
	s := New(WithAutoHEAD(true))
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, r *Response) error {
		r.Header().Set("X-Test", "yes")
		r.Write([]byte("body"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	resp, err := http.DefaultClient.Do(mustNewRequest(t, http.MethodHead, srv.URL+"/x"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || len(body) != 0 || resp.Header.Get("X-Test") != "yes" {
		t.Fatalf("status=%d body=%q headers=%v", resp.StatusCode, body, resp.Header)
	}
	// HEAD 元数据必须与 GET 一致:此前的吞字节包装层让 CL/CT 双双丢失(CL=-1、CT 空)。
	// HEAD metadata must match GET: the old swallowing wrapper lost both (CL=-1, empty CT).
	if resp.ContentLength != 4 {
		t.Errorf("Content-Length = %d, want 4 (same as GET)", resp.ContentLength)
	}
	if resp.Header.Get("Content-Type") == "" {
		t.Error("Content-Type missing; want the same sniffed value as GET")
	}
}

func TestAutoHEADExplicitRouteWins(t *testing.T) {
	s := New(WithAutoHEAD(true))
	get := func(_ context.Context, _ *Request, r *Response) error { r.Write([]byte("get")); return nil }
	head := func(_ context.Context, _ *Request, r *Response) error { r.Header().Set("X-Explicit", "1"); return nil }
	if err := s.RawHandle(http.MethodGet, "/x", get); err != nil {
		t.Fatal(err)
	}
	if err := s.RawHandle(http.MethodHead, "/x", head); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/x", nil))
	if w.Header().Get("X-Explicit") != "1" {
		t.Fatal("explicit HEAD route was not selected")
	}
}

// TestAutoHEADSurvivesExplicitHEADRoutes 覆盖回退从"按树"改为"按路径"的核心:此前只要
// 存在任意一条显式 HEAD 路由(Health/Ready/Static/File 都会注册),autoHEAD 对其余所有
// 路径整体失效(HEAD /x → 405)。
// TestAutoHEADSurvivesExplicitHEADRoutes covers the per-tree → per-path fallback
// change: previously ANY explicit HEAD route (Health/Ready/Static/File all
// register one) killed autoHEAD for every other path (HEAD /x → 405).
func TestAutoHEADSurvivesExplicitHEADRoutes(t *testing.T) {
	s := New(WithAutoHEAD(true))
	if err := s.RawHandle(http.MethodHead, "/health", func(_ context.Context, _ *Request, r *Response) error {
		r.WriteHeader(http.StatusNoContent)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, r *Response) error {
		r.Header().Set("X-Test", "yes")
		r.Write([]byte("body"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/x", nil))
	if w.Code != http.StatusOK || w.Header().Get("X-Test") != "yes" {
		t.Fatalf("HEAD /x with an explicit HEAD /health registered: status=%d headers=%v, want 200 via GET fallback", w.Code, w.Header())
	}
}

// TestAutoHEADAllowHeaderIncludesHEAD 断言 405 的 Allow 在 autoHEAD 开启且 GET 可匹配时
// 补报 HEAD:Allow 是能力承诺,漏掉实际可服务的方法会误导按它探测的客户端。
// TestAutoHEADAllowHeaderIncludesHEAD asserts that a 405 Allow header reports HEAD
// when autoHEAD is on and GET matches: Allow is a capability promise, and omitting
// a servable method misleads clients probing via it.
func TestAutoHEADAllowHeaderIncludesHEAD(t *testing.T) {
	s := New(WithAutoHEAD(true))
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, r *Response) error {
		r.Write([]byte("ok"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow = %q, want %q", allow, "GET, HEAD")
	}
}

// TestAutoHEADParamRouteFallback 覆盖带路径参数的回退:失败的 HEAD 匹配可能已写入部分
// 参数,回退按 GET 树重新匹配前必须清空,否则参数混叠。
// TestAutoHEADParamRouteFallback covers fallback on a parameterized route: a failed
// HEAD match may have written partial params, which must be cleared before the GET
// tree is rematched, or params interleave.
func TestAutoHEADParamRouteFallback(t *testing.T) {
	s := New(WithAutoHEAD(true))
	if err := s.RawHandle(http.MethodHead, "/users/{id}/posts", func(_ context.Context, _ *Request, r *Response) error {
		r.WriteHeader(http.StatusNoContent)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RawHandle(http.MethodGet, "/users/{name}", func(_ context.Context, req *Request, r *Response) error {
		r.Header().Set("X-Name", req.Params.Get("name"))
		if req.Params.Len() != 1 {
			r.Header().Set("X-Params-Leak", "yes")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/users/alice", nil))
	if w.Code != http.StatusOK || w.Header().Get("X-Name") != "alice" {
		t.Fatalf("status=%d headers=%v, want 200 with X-Name=alice via GET fallback", w.Code, w.Header())
	}
	if w.Header().Get("X-Params-Leak") == "yes" {
		t.Fatal("stale params from the failed HEAD match leaked into the GET fallback")
	}
}

// mustNewRequest 构造真实请求,失败即终止测试。
// mustNewRequest builds a real request, failing the test on error.
func mustNewRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

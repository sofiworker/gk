package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TestGetJSONRoundTrip 验证最基础的一次 JSON GET 往返：base URL 拼接、query 编码、
// SetResult 自动解码。
func TestGetJSONRoundTrip(t *testing.T) {
	var gotQuery, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testUser{ID: 7, Name: "bob"})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var user testUser
	resp, err := c.R().SetQueryParam("page", 2).SetResult(&user).Get("/users")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if gotPath != "/users" || gotQuery != "page=2" {
		t.Fatalf("server saw path=%q query=%q", gotPath, gotQuery)
	}
	if user.ID != 7 || user.Name != "bob" {
		t.Fatalf("decoded user = %+v", user)
	}
	if resp.Result() != any(&user) {
		t.Fatalf("Result() should return the registered target")
	}
}

// TestPostJSONRoundTrip 验证请求体编码与 Content-Type 自动设置。
func TestPostJSONRoundTrip(t *testing.T) {
	var body testUser
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("server decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testUser{ID: 1, Name: body.Name})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var out testUser
	if _, err := c.R().SetJSON(testUser{Name: "alice"}).SetResult(&out).Post("/users"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if body.Name != "alice" || out.Name != "alice" {
		t.Fatalf("roundtrip failed: sent=%+v got=%+v", body, out)
	}
}

// TestNon2xxIsError 验证核心语义决策：非 2xx 默认返回 *Error，且错误里带得走响应。
func TestNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"no such user"}}`)
	}))
	defer srv.Close()

	type apiError struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	var apiErr apiError
	c := New(WithBaseURL(srv.URL))
	resp, err := c.R().SetError(&apiErr).Get("/users/404")

	if err == nil {
		t.Fatal("expected an error for 404")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if ce.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d", ce.StatusCode)
	}
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatal("errors.Is(err, ErrUnexpectedStatus) should hold")
	}
	if ce.HTTPStatus() != http.StatusNotFound {
		t.Fatalf("HTTPStatus() = %d", ce.HTTPStatus())
	}
	if resp == nil || resp.StatusCode() != http.StatusNotFound {
		t.Fatal("the response must still be returned alongside the error")
	}
	if body, _ := resp.String(); !strings.Contains(body, "no such user") {
		t.Fatalf("error body must remain readable, got %q", body)
	}
	if apiErr.Error.Code != "not_found" {
		t.Fatalf("SetError target = %+v", apiErr)
	}
}

// TestAllowAllStatus 验证可以退回生态惯例：显式关闭"非 2xx 即错误"。
func TestAllowAllStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithAllowAllStatus())
	resp, err := c.R().Get("/x")
	if err != nil {
		t.Fatalf("WithAllowAllStatus should not error, got %v", err)
	}
	if resp.StatusCode() != 500 {
		t.Fatalf("status = %d", resp.StatusCode())
	}
}

// TestWithHTTPClient 验证完全接管：本包的传输选项被忽略，用户 client 的 CheckRedirect 生效。
func TestWithHTTPClient(t *testing.T) {
	var hops int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		atomic.AddInt32(&hops, 1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	hc := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// CheckRedirect 由外部 client 自带，因此额外显式声明"3xx 由我自己处理"。
	// The CheckRedirect comes from the external client, so opt in explicitly to
	// handling 3xx ourselves.
	c := NewWithHTTPClient(hc, WithBaseURL(srv.URL), WithAcceptRedirects())
	resp, err := c.R().Get("/redirect")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.StatusCode() != http.StatusFound {
		t.Fatalf("status = %d, want 302 (redirect must not be followed)", resp.StatusCode())
	}
	if atomic.LoadInt32(&hops) != 0 {
		t.Fatal("the redirect target must not have been requested")
	}
	if c.HTTPClient() != hc {
		t.Fatal("HTTPClient() must return the injected instance")
	}
}

// TestWithDialer 验证标准库 dialer 注入：请求确实经自定义 DialContext 发出。
func TestWithDialer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	var dialed int32
	c := New(
		WithBaseURL(srv.URL),
		WithDialer(&net.Dialer{Timeout: 5 * time.Second}),
	)
	// 用自定义 DialContext 记录拨号次数，验证注入点确实接在传输层上。
	base := c.HTTPClient().Transport.(*http.Transport)
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&dialed, 1)
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
	}
	if _, err := c.R().Get("/"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if atomic.LoadInt32(&dialed) == 0 {
		t.Fatal("DialContext was never used")
	}
}

// TestRoundTripperAdapter 验证 Client 可以退化成 http.RoundTripper 塞进别人的 http.Client。
func TestRoundTripperAdapter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":3}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	external := &http.Client{Transport: c.RoundTripper()}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := external.Do(req)
	if err != nil {
		t.Fatalf("external.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var u testUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.ID != 3 {
		t.Fatalf("user = %+v", u)
	}
}

// TestResponseBodyLimit 验证默认的安全上限：超限返回 ErrBodyTooLarge 而不是把整个 body 读进内存。
func TestResponseBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 4096))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithResponseBodyLimit(1024))
	_, err := c.R().Get("/big")
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("err = %v, want ErrBodyTooLarge", err)
	}
}

// TestStreamMode 验证流式与内存模式互斥：流式下 Bytes() 必须报错而不是静默读空流。
func TestStreamMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "streamed-payload")
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	resp, err := c.R().SetStreamResponse().Get("/stream")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !resp.IsStream() {
		t.Fatal("IsStream() should be true")
	}
	if _, err := resp.Bytes(); !errors.Is(err, ErrStreamConsumed) {
		t.Fatalf("Bytes() err = %v, want ErrStreamConsumed", err)
	}
	body, err := io.ReadAll(resp.Body())
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "streamed-payload" {
		t.Fatalf("body = %q", body)
	}
	if err := resp.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestStrictTrailingJSON 验证与 server 侧同源的严格性：JSON 值之后的垃圾内容必须报错。
func TestStrictTrailingJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":1} GARBAGE`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.R().SetResult(&testUser{}).Get("/x")
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("err = %v, want an ErrDecode-wrapped failure", err)
	}
}

// TestSetOutputFile 验证下载落盘路径。
func TestSetOutputFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "file-content")
	}))
	defer srv.Close()

	path := t.TempDir() + "/out.bin"
	c := New(WithBaseURL(srv.URL))
	resp, err := c.R().SetOutputFile(path).Get("/file")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.SavedTo() != path {
		t.Fatalf("SavedTo = %q", resp.SavedTo())
	}
	data, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "file-content" {
		t.Fatalf("file = %q", data)
	}
}

// TestMiddlewareOnionOrder 验证中间件按洋葱序执行，且可以短路。
func TestMiddlewareOnionOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	var order []string
	c := New(WithBaseURL(srv.URL))
	c.Use(
		func(next Handler) Handler {
			return func(ctx context.Context, req *Request) (*Response, error) {
				order = append(order, "outer-before")
				resp, err := next(ctx, req)
				order = append(order, "outer-after")
				return resp, err
			}
		},
		func(next Handler) Handler {
			return func(ctx context.Context, req *Request) (*Response, error) {
				order = append(order, "inner-before")
				resp, err := next(ctx, req)
				order = append(order, "inner-after")
				return resp, err
			}
		},
	)
	if _, err := c.R().Get("/x"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := "outer-before,inner-before,inner-after,outer-after"
	if got := strings.Join(order, ","); got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}

	// 短路：中间件不调用 next 而直接返回合成响应。
	short := c.Clone()
	short.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request) (*Response, error) {
			return &Response{request: req, status: 200, header: http.Header{}, body: []byte("cached")}, nil
		}
	})
	resp, err := short.R().Get("/x")
	if err != nil {
		t.Fatalf("short-circuit Get: %v", err)
	}
	if body, _ := resp.String(); body != "cached" {
		t.Fatalf("body = %q", body)
	}
}

// TestPathParamsAndRawQueryString 验证 {name} 替换与 SetQueryString 的原样附加。
func TestPathParamsAndRawQueryString(t *testing.T) {
	var escapedPath, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.URL.Path 是解码后的形式，验证转义要看 EscapedPath。
		// r.URL.Path is the decoded form; escaping is verified via EscapedPath.
		escapedPath, query = r.URL.EscapedPath(), r.URL.RawQuery
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.R().SetPathParam("id", "a b").SetQueryParam("x", 1).SetQueryString("raw=a%20b").Get("/users/{id}"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if escapedPath != "/users/a%20b" {
		t.Fatalf("escaped path = %q, want /users/a%%20b", escapedPath)
	}
	if query != "x=1&raw=a%20b" {
		t.Fatalf("query = %q", query)
	}

	// 缺少值必须报错，而不是发出一个带花括号的 URL。
	if _, err := c.R().Get("/users/{missing}"); err == nil {
		t.Fatal("a placeholder without a value must fail")
	}
}

// TestNoBaseURL 验证没有 base URL 的相对请求被明确拒绝。
func TestNoBaseURL(t *testing.T) {
	c := New()
	_, err := c.R().Get("/relative")
	if !errors.Is(err, ErrNoBaseURL) {
		t.Fatalf("err = %v, want ErrNoBaseURL", err)
	}
}

// TestTransportError 验证传输层失败时 StatusCode 为 0 且错误可 errors.As 取出。
func TestTransportError(t *testing.T) {
	// 指向一个已关闭的端口，制造连接失败。
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close()

	c := New(WithBaseURL(addr), WithDialTimeout(200*time.Millisecond))
	_, err := c.R().Get("/x")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("error type = %T", err)
	}
	if ce.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0 for a transport failure", ce.StatusCode)
	}
	if errors.Is(err, ErrUnexpectedStatus) {
		t.Fatal("a transport failure must not be classified as a status error")
	}
}

// TestCloneIsolation 验证 Clone 改配置不会污染源 Client。
func TestCloneIsolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("X-Tag"))
	}))
	defer srv.Close()

	base := New(WithBaseURL(srv.URL), WithTimeout(30*time.Second))
	variant := base.Clone(WithTimeout(50*time.Millisecond), WithHeader("X-Tag", "v"))
	if base.HTTPClient().Timeout != 30*time.Second {
		t.Fatalf("source Timeout mutated to %v", base.HTTPClient().Timeout)
	}
	if variant.HTTPClient().Timeout != 50*time.Millisecond {
		t.Fatalf("variant Timeout = %v", variant.HTTPClient().Timeout)
	}
	resp, err := variant.R().Get("/x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if body, _ := resp.String(); body != "v" {
		t.Fatalf("variant header not applied, body = %q", body)
	}
}

// TestDoHTTPAndHTTPRequest 验证与标准库的双向互转。
func TestDoHTTPAndHTTPRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":9,"name":"zed"}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))

	// 标准库请求 → 本包编排
	stdReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/std", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.DoHTTP(context.Background(), stdReq)
	if err != nil {
		t.Fatalf("DoHTTP: %v", err)
	}
	var u testUser
	if err := resp.JSON(&u); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if u.ID != 9 {
		t.Fatalf("user = %+v", u)
	}

	// 本包请求 → 标准库请求
	built, err := c.R().SetMethod(http.MethodGet).SetHeader("X-A", "1").SetQueryParam("q", "v").HTTPRequest(context.Background())
	if err != nil {
		t.Fatalf("HTTPRequest: %v", err)
	}
	if built.URL.Query().Get("q") != "v" || built.Header.Get("X-A") != "1" {
		t.Fatalf("built request = %s %s", built.URL, built.Header)
	}
}

// TestHooksFire 验证 before/after/success/error 四类钩子的触发时机。
func TestHooksFire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	var before, after, success, failure int
	c := New(WithBaseURL(srv.URL))
	c.OnBeforeRequest(func(context.Context, *Request) error { before++; return nil })
	c.OnAfterResponse(func(context.Context, *Response) error { after++; return nil })
	c.OnSuccess(func(context.Context, *Response) { success++ })
	c.OnError(func(context.Context, error, *Response) { failure++ })

	if _, err := c.R().Get("/ok"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := c.R().Get("/fail"); err == nil {
		t.Fatal("expected failure")
	}
	if before != 2 || after != 2 || success != 1 || failure != 1 {
		t.Fatalf("hooks: before=%d after=%d success=%d failure=%d", before, after, success, failure)
	}
}

// TestErrorDecoder 验证结构化错误体的显式接入点（本包对错误格式零假设）。
func TestErrorDecoder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"dup"}`)
	}))
	defer srv.Close()

	var seen string
	c := New(
		WithBaseURL(srv.URL),
		WithErrorDecoder(func(resp *Response, e *Error) error {
			var payload struct {
				Code string `json:"code"`
			}
			if err := resp.JSON(&payload); err != nil {
				return err
			}
			seen = payload.Code
			return nil
		}),
	)
	_, err := c.R().Get("/dup")
	if err == nil {
		t.Fatal("expected an error")
	}
	if seen != "dup" {
		t.Fatalf("error decoder saw %q", seen)
	}
}

// TestResponseBodyClosed 用自定义 RoundTripper 计数 body 的关闭，验证资源契约。
func TestResponseBodyClosed(t *testing.T) {
	var closes int32
	body := &countingReadCloser{Reader: strings.NewReader("payload"), closes: &closes}
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       body,
			Request:    req,
		}, nil
	})
	c := New(WithTransport(rt))
	resp, err := c.R().Get("http://example.invalid/x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got, _ := resp.String(); got != "payload" {
		t.Fatalf("body = %q", got)
	}
	if atomic.LoadInt32(&closes) != 1 {
		t.Fatalf("body closed %d times, want exactly 1", closes)
	}
}

type countingReadCloser struct {
	io.Reader
	closes *int32
}

func (c *countingReadCloser) Close() error {
	atomic.AddInt32(c.closes, 1)
	return nil
}

// readFile 读取文件内容。
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

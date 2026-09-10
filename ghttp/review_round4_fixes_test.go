package ghttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// ===========================================================================
// 第四轮评审修复的回归测试。每个测试对应 REVIEW.md 第四轮的一个编号项。
// Regression tests for the round-4 review fixes; each test maps to one
// numbered item in REVIEW.md's round-4 section.
// ===========================================================================

// --- S1': typed-nil StatusCoder 不得击穿 ServeHTTP ---

// typedNilStatusErr 的两个方法都解引用接收者:typed-nil 时调用即 panic,复现
// `var e *typedNilStatusErr; return e` 这一业务经典失误。
// Both methods dereference the receiver: calling them on a typed nil panics,
// reproducing the classic `var e *T; return e` mistake.
type typedNilStatusErr struct{ detail string }

func (e *typedNilStatusErr) Error() string   { return e.detail }
func (e *typedNilStatusErr) HTTPStatus() int { return len(e.detail) + 400 }

func TestClassifyError_TypedNilStatusCoderFallsBackTo500(t *testing.T) {
	// 细节回传开启,连 Error() 的 panic 防护也一并覆盖。
	// Details enabled so the Error() guard is exercised too.
	s := New(WithExposeErrorDetails(true))
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, _ *Response) error {
		var e *typedNilStatusErr
		return e // typed-nil:接口非 nil,内部指针 nil / non-nil interface, nil pointer
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("panic escaped ServeHTTP: %v", rec)
		}
	}()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// --- M2': Run 监听失败后 Server 必须完全回到可注册状态 ---

func TestRunListenFailureAllowsReRegistration(t *testing.T) {
	s := New()
	if err := s.RawHandle(http.MethodGet, "/a", func(_ context.Context, _ *Request, r *Response) error {
		r.Write([]byte("a"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run("256.256.256.256:99999"); err == nil {
		t.Fatal("Run on an invalid address should fail")
	}
	// 此前 endRun 只回退 state 不回退 mux.serving,这里会收到
	// ErrRegistrationAfterStart,Server 成为不可注册的半僵尸。
	// endRun used to reset state but not mux.serving, so this returned
	// ErrRegistrationAfterStart, leaving a half-zombie server.
	if err := s.RawHandle(http.MethodGet, "/b", func(_ context.Context, _ *Request, r *Response) error {
		r.Write([]byte("b"))
		return nil
	}); err != nil {
		t.Fatalf("registration after a failed Run should succeed, got: %v", err)
	}
}

// --- M3': form 路径的 MaxBytesError 必须映射 413 ---

func TestFormBodyOversizeChunkedReturns413(t *testing.T) {
	s := New()
	s.Use(LimitBody(10))
	type F struct {
		A string `form:"a"`
	}
	if err := PostBody(s, "/f", FormBody[F](), JSON[string](), func(_ context.Context, f F) (string, error) {
		return f.A, nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	// chunked(CL=-1)绕过 LimitBody 的 Content-Length 分支,走 MaxBytesReader,
	// 超限错误由 ParseForm 冒出——此前被 `%w: %v` 打平成 400。
	// chunked (CL=-1) skips LimitBody's Content-Length branch and hits
	// MaxBytesReader; the error surfaces from ParseForm — previously flattened
	// to 400 by `%w: %v`.
	body := "a=" + strings.Repeat("x", 100)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/f", io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ContentLength = -1
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s, want 413", resp.StatusCode, b)
	}
}

// --- M5': multipart 总量帽(内存 + 落盘) ---

func TestFormBodyMultipartOversizeReturns413(t *testing.T) {
	type F struct {
		A string `form:"a"`
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("f", "big.bin")
	// 超过 defaultMaxMultipartBytes(64MiB)的总量。
	// A total beyond defaultMaxMultipartBytes (64 MiB).
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 65; i++ {
		fw.Write(chunk)
	}
	w.WriteField("a", "v")
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	err := (formCodec{}).Decode(&Request{Request: req}, &F{})
	if !errors.Is(err, ErrRequestEntityTooLarge) {
		t.Fatalf("err = %v, want ErrRequestEntityTooLarge", err)
	}
}

// --- M8': 表单端点缺 Content-Type 必须 415,不得静默解出零值 ---

func TestFormBodyMissingContentTypeReturns415(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("a=1"))
	req.Header.Del("Content-Type")
	type F struct {
		A string `form:"a"`
	}
	err := (formCodec{}).Decode(&Request{Request: req}, &F{})
	if !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("err = %v, want ErrUnsupportedMediaType", err)
	}
}

// --- M4': gzip 中间件不得把 1xx informational 当最终决策 ---

func TestGzip1xxInformationalKeepsFinalStatus(t *testing.T) {
	s := New()
	s.Use(Gzip())
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, r *Response) error {
		r.WriteHeader(http.StatusEarlyHints) // 103
		r.Header().Set("Content-Type", "application/json")
		r.WriteHeader(http.StatusCreated) // 201:此前被 103 抢占丢失 / previously lost to the 103
		r.Write([]byte(`{"ok":true}`))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (the final status after the 103)", resp.StatusCode)
	}
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", resp.Header.Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}
}

// --- M6': 两个 collect 的注册期防护对称 ---

type m6inner string // 未导出具名类型,内嵌后字段未导出 / unexported named type, unexported when embedded

func TestBindPlanRejectsTaggedUnexportedAnonymous(t *testing.T) {
	type P struct {
		m6inner `query:"x"`
	}
	if _, err := buildBindPlan(reflect.TypeOf(P{})); err == nil {
		t.Fatal("a tagged unexported anonymous field must be rejected at registration")
	}
}

type m6embedded struct {
	V string `form:"v"`
}

func TestFormPlanRejectsUnexportedEmbeddedPointer(t *testing.T) {
	type body struct {
		*m6embedded // 未导出内嵌指针 / unexported embedded pointer
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("v=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	err := (formCodec{}).Decode(&Request{Request: req}, &body{})
	if err == nil {
		t.Fatal("an unexported embedded pointer must be rejected, not panic at request time")
	}
}

// --- M7': 限流默认 key 的 IPv6 /64 聚合 ---

func TestRateLimitIPKeyAggregatesIPv6(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"192.0.2.7", "192.0.2.7"},                    // IPv4 原样 / IPv4 as-is
		{"2001:db8:1:2:aaaa::1", "2001:db8:1:2::/64"}, // 同 /64 聚合 / aggregates
		{"2001:db8:1:2:bbbb::9", "2001:db8:1:2::/64"}, // 轮换地址同桶 / rotated → same bucket
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},      // 不同 /64 不同桶 / different /64
		{"::ffff:192.0.2.7", "::ffff:192.0.2.7"},      // 4-in-6 原样 / 4-in-6 as-is
		{"not-an-ip", "not-an-ip"},                    // 解析失败原样 / unparseable as-is
	}
	for _, c := range cases {
		if got := rateLimitIPKey(c.in); got != c.want {
			t.Errorf("rateLimitIPKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- L5: JSON 尾随垃圾不再物化第二个值;XML 尾随非空白字符数据被拒 ---

func TestXMLTrailingCharDataRejected(t *testing.T) {
	type X struct {
		A string `xml:"a"`
	}
	mk := func(body string) *Request {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/xml")
		return &Request{Request: req}
	}
	// 尾随非空白字符数据:此前 CharData 被无条件跳过而放行。
	// Trailing non-blank character data: previously admitted because CharData
	// was skipped unconditionally.
	if err := (xmlCodec{}).Decode(mk("<X><a>1</a></X>trailing"), &X{}); err == nil {
		t.Fatal("trailing character data must be rejected")
	}
	// 尾随纯空白(格式化换行)仍放行。
	// Trailing pure whitespace (formatting newlines) still passes.
	if err := (xmlCodec{}).Decode(mk("<X><a>1</a></X>\n  \n"), &X{}); err != nil {
		t.Fatalf("trailing whitespace should pass: %v", err)
	}
}

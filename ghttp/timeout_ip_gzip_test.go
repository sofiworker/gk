package ghttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTimeout_DeadlineReachesRequestContext 锁定 P1-3：过去 deadline 只作为闭包参数
// 传给下游，而 raw 端点普遍用 req.Request.Context() 取上下文，看到的仍是无 deadline 的
// 父 ctx —— 于是"已超时"的请求仍能无限阻塞，同时 Timeout 已写了 504。
// Locks the context-injection half of the Timeout fix: the deadline used to reach
// downstream only as the closure argument, while raw handlers read
// req.Request.Context() and saw the deadline-free parent — so an "already timed out"
// request could block forever even though Timeout had written its 504.
func TestTimeout_DeadlineReachesRequestContext(t *testing.T) {
	t.Parallel()

	s := New()
	s.Use(Timeout(30 * time.Millisecond))
	seen := make(chan bool, 1)
	if err := s.RawHandle("GET", "/raw", func(_ context.Context, req *Request, resp *Response) error {
		_, hasDeadline := req.Request.Context().Deadline()
		seen <- hasDeadline
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/raw", nil))
	select {
	case ok := <-seen:
		if !ok {
			t.Fatal("req.Request.Context() has no deadline; the injected ctx never reached the request")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}
}

// TestTimeout_DownstreamCauseIsRetained 锁定 P1-9：原先无条件 return ErrRequestTimeout
// 会把下游错误整个丢弃，排障时只剩"超时"二字。
// Locks the cause-preservation half: returning ErrRequestTimeout unconditionally
// discarded whatever downstream had failed with, leaving only "timed out" to debug.
func TestTimeout_DownstreamCauseIsRetained(t *testing.T) {
	t.Parallel()

	boom := errors.New("database unavailable")
	var got error
	s := New(WithErrorHook(func(_ *http.Request, _ int, e error) { got = e }))
	s.Use(Timeout(10 * time.Millisecond))
	if err := GetParams[struct{}, string](s, "/slow", JSON[string](),
		func(ctx context.Context, _ struct{}) (string, error) {
			<-ctx.Done()
			return "", boom
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/slow", nil))

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504 (the timeout status must not be overridden by the cause)", rec.Code)
	}
	if !errors.Is(got, ErrRequestTimeout) {
		t.Fatalf("hook error %#v does not match ErrRequestTimeout", got)
	}
	if !errors.Is(got, boom) {
		t.Fatalf("the downstream cause was dropped: %v", got)
	}
}

// TestClientIP_MergesRepeatedForwardedHeaders 锁定 P1-4：Header.Get 只看第一行，客户端
// 预置一行 XFF、真实代理再补一行时，攻击者那一行会单独成为 ClientIP。
// Locks P1-4: Header.Get reads only the first line, so when a client preloads one
// XFF line and the real proxy appends another, the attacker's line alone becomes
// ClientIP.
func TestClientIP_MergesRepeatedForwardedHeaders(t *testing.T) {
	t.Parallel()

	s := New(WithTrustedProxies("10.0.0.0/8"))
	var got string
	if err := GetParams[struct{}, string](s, "/ip", JSON[string](),
		func(_ context.Context, _ struct{}) (string, error) { return "x", nil }); err != nil {
		t.Fatalf("register: %v", err)
	}
	// 用中间件读出 ClientIP，验证多行被合并处理。
	// Read ClientIP through a middleware to check the merged chain.
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			got = req.ClientIP()
			return next(ctx, req, resp)
		}
	})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/ip", nil)
	r.RemoteAddr = "10.1.2.3:5555" // 可信代理 / trusted proxy
	// 攻击者先塞一行，代理再追加真实客户端那一行。
	// The attacker plants a line first; the proxy appends the real client line.
	r.Header.Add("X-Forwarded-For", "6.6.6.6")
	r.Header.Add("X-Forwarded-For", "10.9.9.9")
	s.ServeHTTP(rec, r)

	// 合并后链路为 "6.6.6.6,10.9.9.9"，右回溯第一个不可信地址是 6.6.6.6 —— 这正是
	// 只看第一行时会得到的结果，但关键在于代理那一行也参与了判定（若最后一行才是不可信
	// 地址，Header.Get 的版本会答错）。此处直接断言两行都在链路上被解析。
	if got == "" {
		t.Fatal("ClientIP empty; the repeated header was not considered")
	}
	if net.ParseIP(got) == nil {
		t.Fatalf("ClientIP = %q, want a parsed address", got)
	}

	// 决定性用例：伪造行在前、真实客户端在后。只看第一行的实现会返回真实客户端之外的
	// 值，或（当第一行不可解析时）完全忽略该头。
	rec2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/ip", nil)
	r2.RemoteAddr = "10.1.2.3:5555"
	r2.Header.Add("X-Forwarded-For", "not-an-ip") // 攻击者塞的垃圾行 / junk line planted by the client
	r2.Header.Add("X-Forwarded-For", "203.0.113.7")
	s.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec2.Code)
	}
	if got != "203.0.113.7" {
		t.Fatalf("ClientIP = %q, want 203.0.113.7 (only the first header line was consulted)", got)
	}
}

// TestGzip_DoesNotCompressRangeResponses 锁定 P1-5：206 带 Content-Range 时压缩会让
// 声明的字节区间与实际（压缩后）长度彻底对不上。
// Locks P1-5: compressing a 206 with Content-Range makes the declared byte interval
// and the actual (compressed) length irreconcilable.
func TestGzip_DoesNotCompressRangeResponses(t *testing.T) {
	t.Parallel()

	s := New()
	s.Use(Gzip())
	bodyText := strings.Repeat("hello world ", 500)
	if err := GetParams[struct{}, string](s, "/r", JSON[string](),
		func(_ context.Context, _ struct{}) (string, error) { return "", nil }); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Use a pre-built raw route with Range response logic embedded.
	// A pre-built route demonstrates that a Range response with Content-Range stays
	// verbatim regardless of Accept-Encoding.
	if err := s.RawHandle("GET", "/range", func(_ context.Context, req *Request, resp *Response) error {
		h := resp.Header()
		h.Set("Content-Type", "text/plain")
		h.Set("Content-Range", "bytes 0-99/1000")
		resp.WriteHeader(http.StatusPartialContent)
		_, err := resp.Write([]byte(bodyText[:100]))
		return err
	}); err != nil {
		t.Fatalf("register raw: %v", err)
	}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/range", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	s.ServeHTTP(rec, r)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding = %q on a 206; a Range response must stay verbatim", ce)
	}
	if rec.Body.String() != bodyText[:100] {
		t.Fatal("206 body was altered")
	}
}

// TestGzip_FlushCommitsEncodingDecision 锁定 P1-5 的另一半：未定案就 Flush 会先提交不带
// Content-Encoding 的 200 头，之后的 Write 又塞进 gzip 字节 —— 客户端拿到"没有头的
// gzip 体"。
// Locks the other half of P1-5: flushing before the decision commits a 200 header
// without Content-Encoding, and the Write that follows then emits gzip bytes — the
// client receives gzip body bytes with no header saying so.
func TestGzip_FlushCommitsEncodingDecision(t *testing.T) {
	t.Parallel()

	s := New()
	s.Use(Gzip())
	if err := s.RawHandle("GET", "/flush", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/plain")
		resp.Flush() // 先 Flush 再写 / flush first, then write
		_, err := fmt.Fprint(resp, strings.Repeat("abc", 300))
		return err
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/flush", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	s.ServeHTTP(rec, r)

	ce := rec.Header().Get("Content-Encoding")
	hasGzipBytes := rec.Body.Len() >= 2 && rec.Body.Bytes()[0] == 0x1f && rec.Body.Bytes()[1] == 0x8b
	if ce == "" && hasGzipBytes {
		t.Fatal("body is gzip-encoded but Content-Encoding is absent: the client cannot decode it")
	}
	if ce == "gzip" && !hasGzipBytes {
		t.Fatal("Content-Encoding says gzip but the body is not gzip")
	}
}

package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestRetryOn5xx 验证默认策略会对幂等方法的 5xx 重试，并用尽后返回成功结果。
func TestRetryOn5xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":42,"name":"retried"}`)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 3, RetryDelay: time.Millisecond}),
	)
	var user testUser
	resp, err := c.R().SetResult(&user).Get("/x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Attempts() != 3 {
		t.Fatalf("Attempts() = %d, want 3", resp.Attempts())
	}
	if user.ID != 42 {
		t.Fatalf("user = %+v", user)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("server saw %d requests, want 3", got)
	}
}

// TestNoRetryOnPostByDefault 验证默认不对非幂等方法重试：POST 的一次 503 不该变成三次下单。
func TestNoRetryOnPostByDefault(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 3, RetryDelay: time.Millisecond}),
	)
	if _, err := c.R().SetJSON(testUser{Name: "x"}).Post("/orders"); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("server saw %d requests, want exactly 1 (POST must not be retried by default)", got)
	}
}

// TestRetryNonIdempotentOptIn 验证显式放开后 POST 会重试。
func TestRetryNonIdempotentOptIn(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 2, RetryDelay: time.Millisecond, RetryNonIdempotent: true}),
	)
	if _, err := c.R().SetJSON(testUser{Name: "x"}).Post("/orders"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("server saw %d requests, want 2", got)
	}
}

// TestRetryRespectsRetryAfter 验证 Retry-After 覆盖本地退避（这里用 0 秒，避免测试等待）。
func TestRetryRespectsRetryAfter(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		// 故意给一个很大的本地退避：若 Retry-After 未生效，测试会明显变慢甚至超时。
		// A deliberately large local backoff: if Retry-After were ignored, this test
		// would slow down noticeably.
		WithRetry(RetryPolicy{
			MaxRetries:        2,
			RetryDelay:        5 * time.Second,
			RespectRetryAfter: true,
		}),
	)
	start := time.Now()
	if _, err := c.R().Get("/x"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Retry-After: 0 should have been honored, but took %s", elapsed)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

// TestRetryRefusesUnreplayableBody 验证核心安全语义：不可重放的请求体在发出第一次请求【之前】
// 就被拒绝，绝不静默重发一个空 body。
func TestRetryRefusesUnreplayableBody(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 3, RetryDelay: time.Millisecond}),
	)
	// strings.Reader 不是 io.Seeker，无法归零重放。
	// strings.Reader is not an io.Seeker and cannot be rewound.
	_, err := c.R().SetBodyReader(io.NopCloser(bytes.NewBufferString("payload")), "text/plain").Post("/x")
	if !errors.Is(err, ErrBodyNotReplayable) {
		t.Fatalf("err = %v, want ErrBodyNotReplayable", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 0 {
		t.Fatalf("the server saw %d requests; an unreplayable body must be refused before the first attempt", got)
	}
}

// TestRetryReplaysSeekableBody 验证 io.ReadSeeker 型请求体在每次重试前被归零，重放内容一致。
func TestRetryReplaysSeekableBody(t *testing.T) {
	var attempts int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(data))
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 2, RetryDelay: time.Millisecond, RetryNonIdempotent: true}),
	)
	if _, err := c.R().SetBodyReader(bytes.NewReader([]byte("first-payload")), "text/plain").Post("/x"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(bodies))
	}
	for i, b := range bodies {
		if b != "first-payload" {
			t.Fatalf("attempt %d body = %q, want the original payload", i+1, b)
		}
	}
}

// TestRetryStopsOnContextCancel 验证 ctx 取消后立即停止，不再发起新的尝试。
func TestRetryStopsOnContextCancel(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 10, RetryDelay: 50 * time.Millisecond}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := c.R().SetContext(ctx).Get("/x"); err == nil {
		t.Fatal("expected an error")
	}
	// 首次 + 最多一次重试后 ctx 到期；绝不该接近 11 次。
	// First attempt plus at most one retry before the deadline; nowhere near 11.
	if got := atomic.LoadInt32(&attempts); got > 3 {
		t.Fatalf("server saw %d requests; cancellation should stop the loop early", got)
	}
}

// TestRetryHookAndResponseAttempts 验证 OnRetry 钩子与 Attempts 计数。
func TestRetryHookAndResponseAttempts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	var hookCalls int32
	var lastDelay time.Duration
	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{
			MaxRetries: 2,
			RetryDelay: time.Millisecond,
			OnRetry: func(attempt int, delay time.Duration, resp *Response, err error) {
				atomic.AddInt32(&hookCalls, 1)
				lastDelay = delay
			},
		}),
	)
	resp, err := c.R().Get("/x")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&hookCalls); got != 2 {
		t.Fatalf("OnRetry called %d times, want 2", got)
	}
	if lastDelay <= 0 {
		t.Fatalf("delay = %s, want a positive backoff", lastDelay)
	}
	if resp == nil || resp.Attempts() != 3 {
		t.Fatalf("Attempts() = %v, want 3 (1 + 2 retries)", resp)
	}
}

// TestPerRequestRetryOverride 验证请求级 SetRetry 覆盖 Client 级策略。
func TestPerRequestRetryOverride(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 5, RetryDelay: time.Millisecond}),
	)
	// 请求级把重试关掉。
	// The request-level policy turns retries off.
	if _, err := c.R().SetRetry(RetryPolicy{}).Get("/x"); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1 (request-level override)", got)
	}
}

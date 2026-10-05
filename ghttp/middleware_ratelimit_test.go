package ghttp

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type rlTestClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *rlTestClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *rlTestClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func rlCall(h Handler, remote string) (*httptest.ResponseRecorder, error) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	rec := httptest.NewRecorder()
	return rec, h(r.Context(), &Request{Raw: r}, &Response{Writer: rec})
}

func rlOK(context.Context, *Request, *Response) error { return nil }

func TestRateLimit_BurstRefillRetryAfter(t *testing.T) {
	clk := &rlTestClock{t: time.Unix(1000, 0)}
	h := RateLimit(2, 3, WithRateLimitClock(clk.now))(rlOK)
	for i := 0; i < 3; i++ {
		if _, err := rlCall(h, "1.1.1.1:1"); err != nil {
			t.Fatalf("req %d: %v", i, err)
		}
	}
	rec, err := rlCall(h, "1.1.1.1:2")
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("err=%v", err)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" { // 0.5s -> 1
		t.Fatalf("Retry-After=%q", got)
	}
	// 其他键不受影响 / other keys unaffected
	if _, err := rlCall(h, "2.2.2.2:1"); err != nil {
		t.Fatal(err)
	}
	clk.add(500 * time.Millisecond)
	if _, err := rlCall(h, "1.1.1.1:3"); err != nil {
		t.Fatal(err)
	}
	if _, err := rlCall(h, "1.1.1.1:3"); err == nil {
		t.Fatal("expected limit")
	}
}

func TestRateLimit_RetryAfterRoundsUp(t *testing.T) {
	clk := &rlTestClock{t: time.Unix(1000, 0)}
	h := RateLimit(0.4, 1, WithRateLimitClock(clk.now))(rlOK) // 2.5s
	_, _ = rlCall(h, "1.1.1.1:1")
	rec, _ := rlCall(h, "1.1.1.1:1")
	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After=%q", got)
	}
}

func TestRateLimit_CustomKey(t *testing.T) {
	h := RateLimit(1, 1, WithRateLimitKey(func(*Request) string { return "same" }))(rlOK)
	_, _ = rlCall(h, "1.1.1.1:1")
	if _, err := rlCall(h, "9.9.9.9:1"); err == nil {
		t.Fatal("expected shared key limit")
	}
}

func TestRateLimit_IdleCleanup(t *testing.T) {
	clk := &rlTestClock{t: time.Unix(1000, 0)}
	l := &rlLimiter{rate: 1, burst: 1, idleTTL: time.Minute, buckets: map[string]*rlBucket{}}
	l.allow("a", clk.now())
	l.allow("b", clk.now())
	clk.add(2 * time.Minute)
	l.allow("c", clk.now())
	if len(l.buckets) != 1 {
		t.Fatalf("buckets=%d", len(l.buckets))
	}
}

func TestRateLimit_Concurrent(t *testing.T) {
	clk := &rlTestClock{t: time.Unix(1000, 0)}
	h := RateLimit(0, 50, WithRateLimitClock(clk.now))(rlOK)
	var ok int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rlCall(h, "1.1.1.1:1"); err == nil {
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()
	if ok != 50 {
		t.Fatalf("ok=%d want 50", ok)
	}
}

func BenchmarkRateLimit(b *testing.B) {
	h := RateLimit(1e9, 1<<30)(rlOK)
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.1.1.1:1"
	req := &Request{Raw: r}
	resp := &Response{Writer: httptest.NewRecorder()}
	ctx := r.Context()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = h(ctx, req, resp)
		}
	})
}

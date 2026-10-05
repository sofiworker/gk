package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func infReq(ctx context.Context) (*Request, *Response, *httptest.ResponseRecorder) {
	r := httptest.NewRequest("GET", "/x", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	return &Request{Raw: r}, &Response{Writer: rec}, rec
}

func TestInFlightPassThroughWhenDisabled(t *testing.T) {
	called := false
	h := MaxInFlight(0)(func(context.Context, *Request, *Response) error { called = true; return nil })
	req, resp, _ := infReq(context.Background())
	if err := h(context.Background(), req, resp); err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestInFlightRejectsOverLimit(t *testing.T) {
	block, started := make(chan struct{}), make(chan struct{})
	h := MaxInFlight(1, WithInFlightRetryAfter(1500*time.Millisecond))(func(context.Context, *Request, *Response) error {
		close(started)
		<-block
		return nil
	})
	done := make(chan error, 1)
	go func() {
		req, resp, _ := infReq(context.Background())
		done <- h(context.Background(), req, resp)
	}()
	<-started
	req, resp, rec := infReq(context.Background())
	err := h(context.Background(), req, resp)
	if !errors.Is(err, ErrOverloaded) || !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want 2", got)
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestInFlightSlotReleased(t *testing.T) {
	h := MaxInFlight(1)(func(context.Context, *Request, *Response) error { return errors.New("boom") })
	for i := 0; i < 3; i++ {
		req, resp, _ := infReq(context.Background())
		if err := h(context.Background(), req, resp); errors.Is(err, ErrOverloaded) {
			t.Fatalf("slot leaked on iteration %d", i)
		}
	}
}

func TestInFlightNoRetryAfterWhenDisabled(t *testing.T) {
	block, started := make(chan struct{}), make(chan struct{})
	h := MaxInFlight(1, WithInFlightRetryAfter(0))(func(context.Context, *Request, *Response) error {
		close(started)
		<-block
		return nil
	})
	go func() {
		req, resp, _ := infReq(context.Background())
		_ = h(context.Background(), req, resp)
	}()
	<-started
	req, resp, rec := infReq(context.Background())
	if err := h(context.Background(), req, resp); !errors.Is(err, ErrOverloaded) {
		t.Fatal(err)
	}
	close(block)
	if rec.Header().Get("Retry-After") != "" {
		t.Error("Retry-After should be absent")
	}
}

func TestInFlightWaitSucceeds(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	h := MaxInFlight(1, WithInFlightWait(2*time.Second))(func(context.Context, *Request, *Response) error {
		started <- struct{}{}
		<-release
		return nil
	})
	errs := make(chan error, 2)
	run := func() {
		req, resp, _ := infReq(context.Background())
		errs <- h(context.Background(), req, resp)
	}
	go run()
	<-started
	go run()
	time.Sleep(50 * time.Millisecond)
	release <- struct{}{}
	<-started // 第二个拿到名额 / second got the slot
	release <- struct{}{}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestInFlightWaitTimeoutAndCancel(t *testing.T) {
	block, started := make(chan struct{}), make(chan struct{})
	h := MaxInFlight(1, WithInFlightWait(200*time.Millisecond))(func(context.Context, *Request, *Response) error {
		close(started)
		<-block
		return nil
	})
	go func() {
		req, resp, _ := infReq(context.Background())
		_ = h(context.Background(), req, resp)
	}()
	<-started
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(10*time.Millisecond, cancel)
	req, resp, _ := infReq(ctx)
	if err := h(ctx, req, resp); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err = %v", err)
	}

	req, resp, _ = infReq(context.Background())
	if err := h(context.Background(), req, resp); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("timeout err = %v", err)
	}
}

func TestInFlightConcurrentStress(t *testing.T) {
	const limit, total = 5, 200
	var cur, peak, ok, rejected atomic.Int64
	h := MaxInFlight(limit)(func(context.Context, *Request, *Response) error {
		c := cur.Add(1)
		for {
			p := peak.Load()
			if c <= p || peak.CompareAndSwap(p, c) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		cur.Add(-1)
		return nil
	})
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, resp, _ := infReq(context.Background())
			if err := h(context.Background(), req, resp); err == nil {
				ok.Add(1)
			} else if errors.Is(err, ErrOverloaded) {
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > limit {
		t.Errorf("peak = %d > %d", peak.Load(), limit)
	}
	if ok.Load()+rejected.Load() != total || ok.Load() == 0 {
		t.Errorf("ok=%d rejected=%d", ok.Load(), rejected.Load())
	}
}

func TestInFlightServer503(t *testing.T) {
	block, started := make(chan struct{}), make(chan struct{})
	s := NewServer()
	s.Use(MaxInFlight(1))
	if err := s.Register(Raw(http.MethodGet, "/x", func(context.Context, *Request, *Response) error {
		close(started)
		<-block
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { coreDo(s, "GET", "/x", ""); close(done) }()
	<-started
	rec := coreDo(s, "GET", "/x", "")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("code=%d headers=%v", rec.Code, rec.Header())
	}
	close(block)
	<-done
}

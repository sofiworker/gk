package ghttp

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/ghttp/wire"
)

// sseTestServer 启动一个由 fn 驱动 SSEWriter 的 httptest 服务器。
func sseTestServer(t *testing.T, fn func(ctx context.Context, w *SSEWriter)) *httptest.Server {
	t.Helper()
	s := NewServer()
	err := s.Register(Raw(http.MethodGet, "/sse", func(ctx context.Context, _ *Request, resp *Response) error {
		w, err := NewSSEWriter(resp)
		if err != nil {
			return err
		}
		fn(ctx, w)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return ts
}

func TestSSEWriterRoundTrip(t *testing.T) {
	events := []wire.SSEEvent{
		{Data: "hello"},
		{ID: "7", Name: "tick", Data: "line1\nline2\r\nline3"},
		{Name: "r", Data: "x", Retry: 1500 * time.Millisecond},
	}
	ts := sseTestServer(t, func(_ context.Context, w *SSEWriter) {
		for _, ev := range events {
			if err := w.Send(ev); err != nil {
				t.Errorf("Send: %v", err)
			}
		}
		_ = w.Comment("hb\nsecond")
		_ = w.SendData("last")
	})
	resp, err := http.Get(ts.URL + "/sse")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers: %v", resp.Header)
	}
	br := bufio.NewReader(resp.Body)
	want := []wire.SSEEvent{
		{Data: "hello"},
		{ID: "7", Name: "tick", Data: "line1\nline2\nline3"},
		{Name: "r", Data: "x", Retry: 1500 * time.Millisecond},
		{Data: "last"},
	}
	for i, w := range want {
		got, err := wire.ReadSSEEvent(br)
		if err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		got.Comment = ""
		if got.Name == wire.DefaultSSEEventName {
			got.Name = ""
		}
		if got != w {
			t.Errorf("event %d = %+v, want %+v", i, got, w)
		}
	}
	if _, err := wire.ReadSSEEvent(br); !errors.Is(err, io.EOF) {
		t.Errorf("tail err = %v", err)
	}
}

func TestSSEWriterInjection(t *testing.T) {
	ts := sseTestServer(t, func(_ context.Context, w *SSEWriter) {
		_ = w.Send(wire.SSEEvent{ID: "1\ndata:INJECTED", Name: "a\r\nevent:evil", Data: "real"})
		_ = w.Comment("x\ndata:FORGED")
	})
	resp, err := http.Get(ts.URL + "/sse")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	if strings.Contains(body, "\ndata:INJECTED") || strings.Contains(body, "\nevent:evil") || strings.Contains(body, "\ndata:FORGED") {
		t.Fatalf("injection succeeded: %q", body)
	}
	got, err := wire.ReadSSEEvent(bufio.NewReader(strings.NewReader(body)))
	if err != nil || got.Data != "real" || got.ID != "1data:INJECTED" {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

// ssewNoFlush 是不支持 Flush 的 ResponseWriter。
type ssewNoFlush struct {
	h    http.Header
	code int
}

func (w *ssewNoFlush) Header() http.Header         { return w.h }
func (w *ssewNoFlush) Write(b []byte) (int, error) { return len(b), nil }
func (w *ssewNoFlush) WriteHeader(c int)           { w.code = c }

func TestSSEWriterNotSupported(t *testing.T) {
	nf := &ssewNoFlush{h: http.Header{}}
	_, err := NewSSEWriter(&Response{Writer: nf})
	if !errors.Is(err, ErrSSENotSupported) {
		t.Fatalf("err = %v", err)
	}
	if nf.code != 0 {
		t.Errorf("wrote status %d before failing", nf.code)
	}
	if _, err := NewSSEWriter(nil); !errors.Is(err, ErrSSENotSupported) {
		t.Errorf("nil: %v", err)
	}
	// httptest.ResponseRecorder 支持 Flush。
	if _, err := NewSSEWriter(&Response{Writer: httptest.NewRecorder()}); err != nil {
		t.Errorf("recorder: %v", err)
	}
}

func TestSSEWriterKeepAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(&Response{Writer: rec})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.KeepAlive(ctx, 20*time.Millisecond) }()
	time.Sleep(110 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("KeepAlive did not return")
	}
	if n := strings.Count(rec.Body.String(), ":keepalive\n\n"); n < 2 {
		t.Errorf("heartbeats = %d, body %q", n, rec.Body.String())
	}
	// interval<=0 只等待 ctx。
	c2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	if err := w.KeepAlive(c2, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("zero interval err = %v", err)
	}
}

func TestSSEWriterConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(&Response{Writer: rec})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = w.KeepAlive(ctx, time.Millisecond) }()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = w.SendData("d")
			}
		}()
	}
	time.Sleep(30 * time.Millisecond)
	cancel()
	wg.Wait()
	// 每个事件应完整成帧：没有交错的半行。
	for _, frame := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n\n") {
		if frame != "data:d" && frame != ":keepalive" {
			t.Fatalf("interleaved frame %q", frame)
		}
	}
}

func TestSSEWriterKeepAliveStopsOnWriteError(t *testing.T) {
	fw := &ssewFailWriter{ResponseRecorder: httptest.NewRecorder()}
	w, err := NewSSEWriter(&Response{Writer: fw})
	if err != nil {
		t.Fatal(err)
	}
	fw.fail = true
	if err := w.KeepAlive(context.Background(), 5*time.Millisecond); err == nil {
		t.Fatal("expected write error")
	}
}

type ssewFailWriter struct {
	*httptest.ResponseRecorder
	fail bool
}

func (w *ssewFailWriter) Write(b []byte) (int, error) {
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(b)
}

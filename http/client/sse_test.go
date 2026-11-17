package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sseServer 是一个最小的 SSE 端点：依次写出若干帧并冲刷。
// sseServer is a minimal SSE endpoint writing and flushing a few frames.
func sseServer(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter is not a Flusher")
			return
		}
		for _, frame := range frames {
			if _, err := io.WriteString(w, frame); err != nil {
				return
			}
			flusher.Flush()
		}
	}))
}

// TestSSEStreamBasic 验证逐事件迭代、事件名、多行 data 与流结束。
func TestSSEStreamBasic(t *testing.T) {
	srv := sseServer(t,
		"id: 1\nevent: greeting\ndata: hello\n\n",
		": heartbeat\n\n",
		"data: line1\ndata: line2\n\n",
		"retry: 1500\ndata: with-retry\n\n",
	)
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	stream, err := c.SSE(context.Background(), "/events")
	if err != nil {
		t.Fatalf("SSE: %v", err)
	}
	defer func() { _ = stream.Close() }()

	ev1, err := stream.Next()
	if err != nil {
		t.Fatalf("Next 1: %v", err)
	}
	if ev1.ID != "1" || ev1.Name != "greeting" || ev1.Data != "hello" {
		t.Fatalf("event 1 = %+v", ev1)
	}

	// 心跳注释不产生事件。
	// A heartbeat comment yields no event.
	ev2, err := stream.Next()
	if err != nil {
		t.Fatalf("Next 2: %v", err)
	}
	if ev2.Data != "line1\nline2" || ev2.Name != "message" {
		t.Fatalf("event 2 = %+v (multi-line data must be joined with \\n)", ev2)
	}

	ev3, err := stream.Next()
	if err != nil {
		t.Fatalf("Next 3: %v", err)
	}
	if ev3.Retry != 1500*time.Millisecond {
		t.Fatalf("retry = %s, want 1.5s", ev3.Retry)
	}

	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("final Next err = %v, want io.EOF", err)
	}
}

// TestSSELastEventID 验证上一次事件 ID 被记录并可供重连续传。
func TestSSELastEventID(t *testing.T) {
	srv := sseServer(t, "id: abc\ndata: one\n\n", "data: two\n\n")
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	stream, err := c.SSE(context.Background(), "/events")
	if err != nil {
		t.Fatalf("SSE: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if _, err := stream.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	if stream.LastEventID() != "abc" {
		t.Fatalf("LastEventID = %q", stream.LastEventID())
	}
	if _, err := stream.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	// 未声明 id 的事件保持上一次的值。
	// An event without an id keeps the previous value.
	if stream.LastEventID() != "abc" {
		t.Fatalf("LastEventID = %q after an event without id", stream.LastEventID())
	}
}

// TestSSERejectsNonEventStream 验证非事件流响应被明确拒绝，而不是当成事件解析。
func TestSSERejectsNonEventStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":"nope"}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.SSE(context.Background(), "/events"); err == nil {
		t.Fatal("expected an error for a non-event-stream response")
	}
}

// TestConsumeSSEReconnectResumesFromLastEventID 验证断线重连，并带上 Last-Event-ID 续传。
func TestConsumeSSEReconnectResumesFromLastEventID(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			_, _ = io.WriteString(w, "id: e1\ndata: first\n\n")
			flusher.Flush()
			return // 断开连接，触发重连 / drop the connection to trigger a reconnect
		}
		if got := r.Header.Get("Last-Event-ID"); got != "e1" {
			t.Errorf("Last-Event-ID = %q, want e1", got)
		}
		_, _ = io.WriteString(w, "id: e2\ndata: second\n\n")
		flusher.Flush()
		if _, err := io.WriteString(w, "id: e3\ndata: stop\n\n"); err != nil {
			return
		}
		flusher.Flush()
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var got []string
	err := c.ConsumeSSE(context.Background(), "/events", func(ev SSEEvent) error {
		got = append(got, ev.Data)
		if ev.Data == "stop" {
			// 回调返回错误即停止消费。
			// Returning an error from the handler stops consumption.
			return errStopConsuming
		}
		return nil
	}, SSEReconnectPolicy{
		MaxRetries:            3,
		Delay:                 time.Millisecond,
		ResumeFromLastEventID: true,
	})
	if !errors.Is(err, errStopConsuming) {
		t.Fatalf("ConsumeSSE err = %v, want errStopConsuming", err)
	}
	want := "first,second,stop"
	if joined := joinStrings(got); joined != want {
		t.Fatalf("events = %s, want %s", joined, want)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("server calls = %d, want 2", calls)
	}
}

// TestConsumeSSEStopsOnContextCancel 验证 ctx 取消后不再重连。
func TestConsumeSSEStopsOnContextCancel(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		// 立即结束连接，迫使客户端重连。
		// End the connection immediately, forcing a reconnect.
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	err := c.ConsumeSSE(ctx, "/events", func(SSEEvent) error { return nil }, SSEReconnectPolicy{
		MaxRetries: 100,
		Delay:      30 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected ctx cancellation to surface as an error")
	}
	if n := atomic.LoadInt32(&calls); n > 5 {
		t.Fatalf("server calls = %d; cancellation should stop reconnecting", n)
	}
}

var errStopConsuming = errors.New("stop consuming")

func joinStrings(in []string) string { return strings.Join(in, ",") }

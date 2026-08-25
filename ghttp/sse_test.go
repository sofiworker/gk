package ghttp

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// SSE 头部:NewSSEWriter 设置 text/event-stream 等响应头并提交 200。
func TestSSEHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &Response{ResponseWriter: rec}
	_ = NewSSEWriter(resp)
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// SSE 线格式:Send/SendEvent/SendMessage 各字段与事件分隔符正确。
func TestSSEWireFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewSSEWriter(&Response{ResponseWriter: rec})

	if err := w.Send("hello"); err != nil {
		t.Fatal(err)
	}
	if err := w.SendEvent("tick", "42"); err != nil {
		t.Fatal(err)
	}
	if err := w.SendMessage(SSEMessage{ID: "7", Event: "update", Data: "x", Retry: 3 * time.Second}); err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()
	want := "data:hello\n\n" +
		"event:tick\ndata:42\n\n" +
		"id:7\nevent:update\nretry:3000\ndata:x\n\n"
	if body != want {
		t.Errorf("SSE body mismatch:\n got=%q\nwant=%q", body, want)
	}
}

// 多行 data:每行独立加 data: 前缀,\r 被裁剪。
func TestSSEMultilineData(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewSSEWriter(&Response{ResponseWriter: rec})
	if err := w.Send("line1\r\nline2\nline3"); err != nil {
		t.Fatal(err)
	}
	want := "data:line1\ndata:line2\ndata:line3\n\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("multiline data:\n got=%q\nwant=%q", got, want)
	}
}

// 空 data 仍产出一条合法事件(一个空 data 行 + 空行)。
func TestSSEEmptyData(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewSSEWriter(&Response{ResponseWriter: rec})
	if err := w.Send(""); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != "data:\n\n" {
		t.Errorf("empty data: got=%q, want %q", got, "data:\n\n")
	}
}

// 注释与心跳:Comment 产出 ":text\n\n",Ping 产出 ":\n\n"。
func TestSSECommentAndPing(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewSSEWriter(&Response{ResponseWriter: rec})
	if err := w.Comment("keep-alive"); err != nil {
		t.Fatal(err)
	}
	if err := w.Ping(); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != ":keep-alive\n\n:\n\n" {
		t.Errorf("comment/ping: got=%q", got)
	}
}

// Flush 计数:每条事件都触发底层 Flush(经计数型 ResponseWriter 验证)。
func TestSSEFlushesPerEvent(t *testing.T) {
	fw := &flushCountWriter{ResponseRecorder: httptest.NewRecorder()}
	w := NewSSEWriter(&Response{ResponseWriter: fw})
	before := fw.flushes // 构造时已 flush 一次(头部)
	_ = w.Send("a")
	_ = w.Send("b")
	if fw.flushes-before != 2 {
		t.Errorf("flush count for 2 events = %d, want 2", fw.flushes-before)
	}
}

// 写错误被记住:一旦底层写失败,后续 Send 短路返回同一错误,Err() 反映之。
func TestSSEWriteErrorSticky(t *testing.T) {
	ew := &errAfterWriter{ResponseRecorder: httptest.NewRecorder(), failAfter: 1}
	w := NewSSEWriter(&Response{ResponseWriter: ew})
	// 第 1 次 Send 成功(headers 的 flush 不走 Write 计数,首个 Write 触发失败计数)。
	_ = w.Send("ok")
	err := w.Send("boom")
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
	if w.Err() == nil {
		t.Error("Err() should reflect the sticky write error")
	}
	// 再次 Send 应短路返回同一错误,不再写。
	if err2 := w.Send("again"); err2 != err {
		t.Errorf("second Send err = %v, want sticky %v", err2, err)
	}
}

// 端到端:经 RawHandle + 真实 net/http 服务器,客户端逐行读到事件流。
func TestSSEEndToEnd(t *testing.T) {
	s := New()
	if err := s.RawHandle(http.MethodGet, "/events", func(ctx context.Context, req *Request, resp *Response) error {
		w := NewSSEWriter(resp)
		for i := 0; i < 3; i++ {
			if err := w.SendEvent("tick", strings.Repeat("x", i+1)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"event:tick", "data:x", "data:xx", "data:xxx"} {
		if !strings.Contains(joined, want) {
			t.Errorf("stream missing %q; got:\n%s", want, joined)
		}
	}
}

// flushCountWriter 统计 Flush 调用次数。
type flushCountWriter struct {
	*httptest.ResponseRecorder
	flushes int
}

func (w *flushCountWriter) Flush() { w.flushes++; w.ResponseRecorder.Flush() }

// errAfterWriter 在第 failAfter 次 Write 起返回错误,模拟客户端断开。
type errAfterWriter struct {
	*httptest.ResponseRecorder
	writes    int
	failAfter int
}

func (w *errAfterWriter) Write(b []byte) (int, error) {
	w.writes++
	if w.writes > w.failAfter {
		return 0, errClientGone
	}
	return w.ResponseRecorder.Write(b)
}

func (w *errAfterWriter) Flush() { w.ResponseRecorder.Flush() }

var errClientGone = &sseTestErr{"client gone"}

type sseTestErr struct{ s string }

func (e *sseTestErr) Error() string { return e.s }

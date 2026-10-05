package ghttp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sofiworker/gk/ghttp/wire"
)

// ErrSSENotSupported 表示底层 ResponseWriter 不支持 Flush，无法做 SSE。
// ErrSSENotSupported means the underlying ResponseWriter cannot Flush, so SSE is impossible.
var ErrSSENotSupported = errors.New("ghttp: SSE not supported: response writer cannot flush")

// SSEWriter 向客户端写 text/event-stream；方法并发安全。
// SSEWriter writes text/event-stream to the client; its methods are concurrency-safe.
type SSEWriter struct {
	mu   sync.Mutex
	resp *Response
	buf  []byte
}

// ssewCanFlush 沿 Unwrap 链探测是否存在 Flush 能力（不产生写入）。
// ssewCanFlush probes the Unwrap chain for flush support without writing anything.
func ssewCanFlush(w http.ResponseWriter) bool {
	for w != nil {
		switch w.(type) {
		case interface{ FlushError() error }, http.Flusher:
			return true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = u.Unwrap()
	}
	return false
}

// NewSSEWriter 设置 SSE 响应头、写 200 并 Flush。底层不支持 Flush 时返回 ErrSSENotSupported，
// 此时尚未写出任何内容，调用方仍可返回普通错误。
// NewSSEWriter sets the SSE headers, writes 200 and flushes. If the underlying writer cannot
// flush it returns ErrSSENotSupported before writing anything, so the caller can still
// return a normal error.
func NewSSEWriter(resp *Response) (*SSEWriter, error) {
	if resp == nil || resp.Writer == nil || !ssewCanFlush(resp.Writer) {
		return nil, ErrSSENotSupported
	}
	h := resp.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	resp.WriteHeader(http.StatusOK)
	if err := resp.FlushError(); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			return nil, ErrSSENotSupported
		}
		return nil, err
	}
	return &SSEWriter{resp: resp}, nil
}

// ssewSplit 按 \r\n、\r、\n 拆行。
// ssewSplit splits on \r\n, \r and \n.
func ssewSplit(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// flushLocked 写出缓冲并 Flush；调用方持锁。
// flushLocked writes the buffer and flushes; the caller holds the lock.
func (w *SSEWriter) flushLocked() error {
	_, err := w.resp.Write(w.buf)
	w.buf = w.buf[:0]
	if err != nil {
		return err
	}
	return w.resp.FlushError()
}

// Send 写出一个事件：Name/ID 去除换行；多行 Data 拆成多条 data 行；Retry>0 时写毫秒；
// Comment 非空时先写注释行。写完即 Flush。
// Send writes one event: Name/ID are stripped of line breaks, multi-line Data becomes several
// data lines, Retry>0 is written in milliseconds, and a non-empty Comment is written as
// comment lines first. It flushes right after.
func (w *SSEWriter) Send(ev wire.SSEEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.buf[:0]
	if ev.Comment != "" {
		for _, l := range ssewSplit(ev.Comment) {
			b = wire.AppendSSEField(b, "", l)
		}
	}
	if ev.Name != "" {
		b = wire.AppendSSEField(b, wire.SSEFieldEvent, wire.StripSSELineBreaks(ev.Name))
	}
	if ev.ID != "" {
		b = wire.AppendSSEField(b, wire.SSEFieldID, wire.StripSSELineBreaks(ev.ID))
	}
	if ev.Retry > 0 {
		b = wire.AppendSSEField(b, wire.SSEFieldRetry, strconv.FormatInt(ev.Retry.Milliseconds(), 10))
	}
	for _, l := range ssewSplit(ev.Data) {
		b = wire.AppendSSEField(b, wire.SSEFieldData, l)
	}
	b = append(b, '\n')
	w.buf = b
	return w.flushLocked()
}

// SendData 发送仅含 data 的事件。
// SendData sends a data-only event.
func (w *SSEWriter) SendData(data string) error {
	return w.Send(wire.SSEEvent{Data: data})
}

// Comment 发送注释行（常用作心跳），多行会拆成多条注释。
// Comment sends comment lines (commonly heartbeats); multi-line text is split.
func (w *SSEWriter) Comment(text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.buf[:0]
	for _, l := range ssewSplit(text) {
		b = wire.AppendSSEField(b, "", l)
	}
	b = append(b, '\n')
	w.buf = b
	return w.flushLocked()
}

// KeepAlive 阻塞直到 ctx 结束，每隔 interval 发送一次注释心跳；写失败（客户端断开）时提前返回。
// interval<=0 时只等待 ctx。返回 ctx.Err() 或写错误。
// KeepAlive blocks until ctx ends, sending a comment heartbeat every interval; it returns
// early on a write failure (client gone). With interval<=0 it only waits for ctx. It
// returns ctx.Err() or the write error.
func (w *SSEWriter) KeepAlive(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := w.Comment("keepalive"); err != nil {
				return err
			}
		}
	}
}

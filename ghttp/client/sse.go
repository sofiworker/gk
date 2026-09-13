package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	internalsse "github.com/sofiworker/gk/ghttp/internal/sse"
	"github.com/sofiworker/gk/gretry"
)

// 本文件实现 client 侧的 Server-Sent Events 消费。线格式解析复用 ghttp/internal/sse
// （与 server 的写端共享同一份规范实现），本文件只管连接建立、迭代与生命周期。
// This file implements client-side Server-Sent Events consumption. Wire-format parsing
// reuses ghttp/internal/sse (one specification implementation shared with the server's
// writer); this file handles connection setup, iteration and lifetime.

// SSEEvent 是一个已解析的 SSE 事件。
// SSEEvent is one parsed SSE event.
type SSEEvent struct {
	// ID 是事件 ID；为空表示本次事件未声明（此时 LastEventID 保持上一次的值）。
	// ID is the event id; empty means this event declared none (LastEventID then keeps
	// the previous value).
	ID string
	// Name 是事件类型，缺省为 "message"。
	// Name is the event type, defaulting to "message".
	Name string
	// Data 是事件数据；多行 data 字段已用 \n 连接。
	// Data is the event payload; multiple data fields are joined with \n.
	Data string
	// Retry 是服务端建议的重连间隔（retry 字段）；为零表示未声明。
	// Retry is the server-suggested reconnect interval (the retry field); zero means
	// undeclared.
	Retry time.Duration
}

// SSEStream 是一个已建立的事件流。它必须在用完后 Close：底层是长连接，不关会一直占用
// 一条连接与一个 goroutine。
// SSEStream is an established event stream. It must be Closed when done: the underlying
// long-lived connection would otherwise hold a connection and a goroutine forever.
type SSEStream struct {
	resp        *Response
	reader      *bufio.Reader
	lastEventID string
	closed      bool
}

// SSE 建立一条 Server-Sent Events 连接并返回可迭代的事件流。
//
// 它固定以 GET + Accept: text/event-stream 发起，并强制流式响应：事件必须边到边处理，
// 不能等整个响应体读完。SSE 是长连接，因此本方法内部关闭重试——流断开后的重连属于调用方
// 的策略（可带上 LastEventID 作 Last-Event-ID 头续传）。
// SSE establishes a Server-Sent Events connection and returns an iterable stream.
//
// It always issues GET with Accept: text/event-stream and forces a streaming response:
// events must be handled as they arrive, never after the whole body is read. SSE is a
// long-lived connection, so retries are disabled here — reconnecting after a drop is the
// caller's policy (optionally resuming with LastEventID as the Last-Event-ID header).
func (c *Client) SSE(ctx context.Context, rawURL string, opts ...RequestOption) (*SSEStream, error) {
	if c == nil {
		return nil, fmt.Errorf("ghttp/client: nil client")
	}
	r := c.R().SetMethod(http.MethodGet).SetURL(rawURL)
	if ctx != nil {
		r.SetContext(ctx)
	}
	r.SetHeader("Accept", "text/event-stream")
	r.SetHeader("Cache-Control", "no-cache")
	r.SetStreamResponse()
	r.SetRetry(RetryPolicy{})
	applyRequestOptions(r, opts)

	resp, err := r.Send()
	if err != nil {
		if resp != nil {
			_ = resp.Close()
		}
		return nil, err
	}
	if ct := mediaTypeOf(resp.Header().Get("Content-Type")); ct != "text/event-stream" {
		_ = resp.Close()
		return nil, fmt.Errorf("%w: expected text/event-stream, got %q", ErrNoCodec, ct)
	}
	return &SSEStream{
		resp:   resp,
		reader: bufio.NewReaderSize(resp.Body(), 4096),
	}, nil
}

// Next 返回下一个事件。返回 io.EOF 表示服务端正常结束了流。
//
// 注释行（心跳）不产生事件：它们被解析器跳过，因此 Next 可能阻塞到下一个真实事件或流结束。
// 需要感知心跳的调用方应给 ctx 一个超时，而不是依赖空事件。
// Next returns the next event; io.EOF means the server ended the stream cleanly.
//
// Comment lines (heartbeats) produce no event — the parser skips them, so Next may block
// until the next real event or the end of the stream. Callers needing heartbeat
// visibility should bound their ctx instead of relying on empty events.
func (s *SSEStream) Next() (SSEEvent, error) {
	ev, err := internalsse.ReadEvent(s.reader)
	if err != nil {
		return SSEEvent{}, err
	}
	if ev.ID != "" {
		s.lastEventID = ev.ID
	}
	return SSEEvent{ID: ev.ID, Name: ev.Name, Data: ev.Data, Retry: ev.Retry}, nil
}

// LastEventID 返回最近一次收到的事件 ID，供重连时作为 Last-Event-ID 头发送以续传。
// LastEventID returns the most recently received event id, to be sent as the
// Last-Event-ID header when reconnecting.
func (s *SSEStream) LastEventID() string { return s.lastEventID }

// Response 返回建立该流的响应（可读状态码与响应头）。
// Response returns the response that established the stream (status and headers).
func (s *SSEStream) Response() *Response { return s.resp }

// Close 关闭事件流。可重复调用。
// Close closes the stream. It is safe to call repeatedly.
func (s *SSEStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.resp.Close()
}

// SSEHandler 处理一个事件；返回非 nil 错误即停止消费。
// SSEHandler processes one event; a non-nil error stops consumption.
type SSEHandler func(ev SSEEvent) error

// SSEReconnectPolicy 描述流断开后的重连策略。
// SSEReconnectPolicy describes how to reconnect after the stream drops.
type SSEReconnectPolicy struct {
	// MaxRetries 是最大重连次数；0 表示不重连。
	// MaxRetries is the maximum number of reconnects; 0 means none.
	MaxRetries int
	// Delay / MaxDelay / Strategy / Jitter 透传给 gretry 的退避计算。
	// Delay / MaxDelay / Strategy / Jitter are passed through to gretry's backoff.
	Delay    time.Duration
	MaxDelay time.Duration
	// ResumeFromLastEventID 为真时，重连请求带上 Last-Event-ID 头续传。
	// ResumeFromLastEventID sends the Last-Event-ID header when reconnecting.
	ResumeFromLastEventID bool
}

// ConsumeSSE 以回调方式消费事件流，并在流断开时按 policy 重连。任一次回调返回错误、或
// ctx 结束，都会停止并把该错误返回（io.EOF 视为流的正常结束，不作为错误）。
//
// 注意：重连是否安全取决于服务端语义。本方法只保证"从 Last-Event-ID 之后续传"这一协议
// 机制被正确使用，不保证服务端不重发事件。
// ConsumeSSE consumes a stream through a callback and reconnects per policy when the
// stream drops. A callback error or a cancelled ctx stops it and returns that error
// (io.EOF counts as a clean end, not an error).
//
// Note that whether reconnecting is safe depends on the server's semantics: this method
// only ensures the Last-Event-ID resumption mechanism is used correctly, not that the
// server never replays events.
func (c *Client) ConsumeSSE(ctx context.Context, rawURL string, handler SSEHandler, policy SSEReconnectPolicy, opts ...RequestOption) error {
	if handler == nil {
		return fmt.Errorf("ghttp/client: nil SSE handler")
	}
	lastID := ""
	var lastErr error

	for attempt := 0; ; attempt++ {
		reqOpts := append([]RequestOption(nil), opts...)
		if policy.ResumeFromLastEventID && lastID != "" {
			reqOpts = append(reqOpts, WithRequestHeader("Last-Event-ID", lastID))
		}
		stream, err := c.SSE(ctx, rawURL, reqOpts...)
		var stopped bool
		if err != nil {
			lastErr = err
		} else {
			stopped, lastErr = consumeStream(stream, handler, &lastID)
			_ = stream.Close()
		}
		// handler 主动停止：立即返回它的错误，不重连。
		// The handler stopped on purpose: return its error immediately, no reconnect.
		if stopped {
			return lastErr
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		// 关键区分：流"正常结束"（io.EOF）同样需要重连——服务端主动断开是长连接的常态，
		// 若把它当作完成就会静默丢失后续事件。
		// The key distinction: a CLEAN end (io.EOF) also warrants a reconnect — a
		// server-side close is normal for a long-lived connection, and treating it as
		// completion would silently drop all later events.
		if attempt >= policy.MaxRetries {
			if lastErr == nil {
				return io.EOF
			}
			return lastErr
		}
		if werr := sleepBackoff(ctx, attempt, policy); werr != nil {
			return werr
		}
	}
}

// consumeStream 逐事件调用 handler。第一个返回值为真表示 handler 主动停止（此时第二个
// 返回值是它的错误）；为假时第二个返回值是流结束或读取失败的原因（io.EOF 表示正常结束）。
// consumeStream invokes handler per event. A true first result means the handler stopped
// on purpose (the second is its error); a false first result means the stream ended or a
// read failed (io.EOF for a clean end).
func consumeStream(stream *SSEStream, handler SSEHandler, lastID *string) (bool, error) {
	for {
		ev, err := stream.Next()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if ev.ID != "" {
			*lastID = ev.ID
		}
		if herr := handler(ev); herr != nil {
			return true, herr
		}
	}
}

// sleepBackoff 用 gretry 的同一套退避计算等待下一次重连，ctx 取消时立即返回。
// sleepBackoff waits for the next reconnect using gretry's backoff computation, returning
// immediately when ctx is cancelled.
func sleepBackoff(ctx context.Context, attempt int, policy SSEReconnectPolicy) error {
	opts := gretry.DefaultErrorHandlingOptions
	if policy.Delay > 0 {
		opts.RetryDelay = policy.Delay
	}
	if policy.MaxDelay > 0 {
		opts.MaxRetryDelay = policy.MaxDelay
	}
	return gretry.Wait(ctx, gretry.NextDelay(attempt, opts))
}

package ghttp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SSEWriter 向响应写入 Server-Sent Events。
// SSEWriter writes Server-Sent Events to a response.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// SSEHandler 处理 SSE 连接。
// SSEHandler handles an SSE connection.
type SSEHandler func(ctx context.Context, params Params, stream *SSEWriter) error

// SSEEvent 表示单个 SSE 事件。
// SSEEvent represents a single SSE event.
type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

func (s *SSEWriter) WriteEvent(event, data string) error {
	if strings.ContainsAny(event, "\r\n") {
		return fmt.Errorf("sse: event name contains newline")
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\n", event); err != nil {
		return err
	}
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if _, err := fmt.Fprintf(s.w, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(s.w, "\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *SSEWriter) WriteEventWithID(event, id, data string) error {
	if strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("sse: event id contains newline")
	}
	if _, err := fmt.Fprintf(s.w, "id: %s\n", id); err != nil {
		return err
	}
	return s.WriteEvent(event, data)
}

func (s *SSEWriter) WriteComment(text string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n", strings.ReplaceAll(text, "\n", " ")); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *SSEWriter) Retry(millis int) error {
	if millis < 0 {
		return fmt.Errorf("sse: retry must be non-negative")
	}
	_, err := fmt.Fprintf(s.w, "retry: %d\n\n", millis)
	if err == nil {
		s.flusher.Flush()
	}
	return err
}

func (s *SSEWriter) WriteJSON(event string, data interface{}) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.WriteEvent(event, string(b))
}

// WriteJSONWithID 写入带显式 id 的 JSON 事件。
// WriteJSONWithID writes a JSON event with an explicit id.
// 客户端可按 Last-Event-ID 续传。
// clients can resume from Last-Event-ID.
func (s *SSEWriter) WriteJSONWithID(event, id string, data interface{}) error {
	if strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("sse: event id contains newline")
	}
	if _, err := fmt.Fprintf(s.w, "id: %s\n", id); err != nil {
		return err
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.WriteEvent(event, string(b))
}

// SSEStream 是客户端 SSE 流。
// SSEStream is a client-side SSE stream.
type SSEStream struct {
	Events  <-chan SSEEvent
	Errors  <-chan error
	closeFn context.CancelFunc
}

// SSEConfig 配置客户端 SSE 行为。
// SSEConfig configures client SSE behavior.
type SSEConfig struct {
	Context       context.Context
	OnEvent       func(SSEEvent)
	OnError       func(error)
	OnConnect     func(*http.Response)
	Reconnect     bool
	RetryInterval time.Duration
	MaxRetries    int
	LastEventID   string
}

func (c *Client) SSE(url string, config ...SSEConfig) (*SSEStream, error) {
	cfg := SSEConfig{}
	if len(config) > 0 {
		cfg = config[0]
	}

	ctx := cfg.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	retryInterval := cfg.RetryInterval
	if retryInterval <= 0 {
		retryInterval = 3 * time.Second
	}

	resolvedURL := resolveClientURL(c.baseURL, url)
	lastEventID := cfg.LastEventID
	resp, err := c.openSSE(ctx, resolvedURL, lastEventID)
	if err != nil {
		cancel()
		return nil, err
	}

	if cfg.OnConnect != nil {
		cfg.OnConnect(resp)
	}

	events := make(chan SSEEvent, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		currentResp := resp
		retries := 0
		for {
			err := readSSE(ctx, currentResp.Body, events, errs, cfg, &lastEventID, &retryInterval)
			_ = currentResp.Body.Close()
			if ctx.Err() != nil {
				return
			}
			emitSSEError(errs, cfg, err)
			if !cfg.Reconnect {
				return
			}

			for {
				if cfg.MaxRetries > 0 && retries >= cfg.MaxRetries {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(retryInterval):
				}
				retries++

				nextResp, err := c.openSSE(ctx, resolvedURL, lastEventID)
				if err != nil {
					emitSSEError(errs, cfg, err)
					continue
				}
				currentResp = nextResp
				if cfg.OnConnect != nil {
					cfg.OnConnect(nextResp)
				}
				break
			}
		}
	}()

	return &SSEStream{
		Events:  events,
		Errors:  errs,
		closeFn: cancel,
	}, nil
}

func (s *SSEStream) Close() {
	if s.closeFn != nil {
		s.closeFn()
	}
}

func (c *Client) openSSE(ctx context.Context, url, lastEventID string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("sse connect failed: %s", resp.Status)
	}
	return resp, nil
}

func readSSE(ctx context.Context, body io.Reader, events chan<- SSEEvent, errs chan<- error, cfg SSEConfig, lastEventID *string, retryInterval *time.Duration) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event SSEEvent

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Text()
		if line == "" {
			emitSSEEvent(ctx, events, cfg, event)
			event = SSEEvent{}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value := splitSSELine(line)
		switch field {
		case "id":
			event.ID = value
			*lastEventID = value
		case "event":
			event.Event = value
		case "data":
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += value
		case "retry":
			if millis, err := strconv.Atoi(value); err == nil && millis >= 0 {
				*retryInterval = time.Duration(millis) * time.Millisecond
			}
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func splitSSELine(line string) (string, string) {
	field, value, ok := strings.Cut(line, ":")
	if !ok {
		return line, ""
	}
	value = strings.TrimPrefix(value, " ")
	return field, value
}

func emitSSEEvent(ctx context.Context, events chan<- SSEEvent, cfg SSEConfig, event SSEEvent) {
	if event.ID == "" && event.Event == "" && event.Data == "" {
		return
	}
	if cfg.OnEvent != nil {
		cfg.OnEvent(event)
	}
	select {
	case events <- event:
	case <-ctx.Done():
	default:
	}
}

func emitSSEError(errs chan<- error, cfg SSEConfig, err error) {
	if err == nil {
		return
	}
	if cfg.OnError != nil {
		cfg.OnError(err)
	}
	select {
	case errs <- err:
	default:
	}
}

func resolveClientURL(baseURL, url string) string {
	if baseURL != "" && !strings.HasPrefix(url, "http") {
		return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(url, "/")
	}
	return url
}

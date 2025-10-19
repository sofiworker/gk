// sse.go - 修复 SSE 客户端支持

package gclient

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SSEEvent 表示一个SSE事件
type SSEEvent struct {
	ID    string
	Event string
	Data  string
	Retry int
}

// SSEHandler SSE事件处理器
type SSEHandler func(event SSEEvent) error

// SSERequest SSE请求配置
type SSERequest struct {
	*Request
	handler     SSEHandler
	reconnect   bool
	retryDelay  time.Duration
	maxRetries  int
	lastEventID string
}

func NewSSERequest(r *Request) *SSERequest {
	return &SSERequest{Request: r}
}

// SetSSEHandler 设置SSE事件处理器
func (r *Request) SetSSEHandler(handler SSEHandler) *SSERequest {
	sseReq := &SSERequest{
		Request:    r,
		handler:    handler,
		reconnect:  true,
		retryDelay: 3 * time.Second,
		maxRetries: 5,
	}
	return sseReq
}

// SetReconnect 设置是否自动重连
func (s *SSERequest) SetReconnect(reconnect bool) *SSERequest {
	s.reconnect = reconnect
	return s
}

// SetRetryDelay 设置重试延迟
func (s *SSERequest) SetRetryDelay(delay time.Duration) *SSERequest {
	s.retryDelay = delay
	return s
}

// SetMaxRetries 设置最大重试次数
func (s *SSERequest) SetMaxRetries(maxRetries int) *SSERequest {
	s.maxRetries = maxRetries
	return s
}

// SetLastEventID 设置最后事件ID（用于恢复连接）
func (s *SSERequest) SetLastEventID(id string) *SSERequest {
	s.lastEventID = id
	return s
}

// Stream 执行SSE流式请求
func (s *SSERequest) Stream(ctx context.Context) error {
	if s.handler == nil {
		return fmt.Errorf("sse handler is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	retries := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.Request.Clone()
		req.SetHeader("Accept", "text/event-stream")
		req.SetHeader("Cache-Control", "no-cache")
		req.SetContext(ctx)
		if s.lastEventID != "" {
			req.SetHeader("Last-Event-ID", s.lastEventID)
		}

		builder := newHTTPRequestBuilder(req, req.client)
		httpReq, err := builder.Build()
		if err != nil {
			return err
		}

		httpResp, err := req.client.executor.Do(httpReq)
		if err != nil {
			if !s.shouldReconnect(retries, err) {
				return err
			}
			retries++
			if err := sleepWithContext(ctx, s.retryDelay); err != nil {
				return err
			}
			continue
		}

		if httpResp.StatusCode != http.StatusOK {
			_ = httpResp.Body.Close()
			err = fmt.Errorf("unexpected status code %d", httpResp.StatusCode)
			if !s.shouldReconnect(retries, err) {
				return err
			}
			retries++
			if err := sleepWithContext(ctx, s.retryDelay); err != nil {
				return err
			}
			continue
		}

		err = s.handleSSEStream(ctx, httpResp.Body)
		_ = httpResp.Body.Close()
		if err == nil {
			return nil
		}
		if !s.shouldReconnect(retries, err) {
			return err
		}
		retries++
		if err := sleepWithContext(ctx, s.retryDelay); err != nil {
			return err
		}
	}
}

// shouldReconnect 判断是否应该重连
func (s *SSERequest) shouldReconnect(retries int, err error) bool {
	return s.reconnect && (s.maxRetries <= 0 || retries < s.maxRetries)
}

// handleSSEStream 处理SSE流数据
func (s *SSERequest) handleSSEStream(ctx context.Context, stream io.Reader) error {
	scanner := bufio.NewScanner(stream)
	buf := make([]byte, 0, 128*1024)
	scanner.Buffer(buf, 512*1024)

	var currentEvent SSEEvent

	for scanner.Scan() {
		line := scanner.Text()

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 空行表示一个事件的结束
		if line == "" {
			if currentEvent.Data != "" {
				// 调用处理器
				if err := s.handler(currentEvent); err != nil {
					return err
				}
				// 重置事件
				currentEvent = SSEEvent{}
			}
			continue
		}

		// 处理注释行（以:开头）
		if strings.HasPrefix(line, ":") {
			continue
		}

		// 分割字段和值
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		field := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch field {
		case "id":
			currentEvent.ID = value
			s.lastEventID = value
		case "event":
			currentEvent.Event = value
		case "data":
			if currentEvent.Data != "" {
				currentEvent.Data += "\n"
			}
			currentEvent.Data += value
		case "retry":
			if n, err := fmt.Sscanf(value, "%d", &currentEvent.Retry); err == nil && n == 1 {
				if currentEvent.Retry > 0 {
					s.retryDelay = time.Duration(currentEvent.Retry) * time.Millisecond
				}
			}
		}
	}

	return scanner.Err()
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

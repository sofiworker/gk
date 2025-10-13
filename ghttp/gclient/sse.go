// sse.go - 修复 SSE 客户端支持

package gclient

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

	return nil
}

// shouldReconnect 判断是否应该重连
func (s *SSERequest) shouldReconnect(retries int, err error) bool {
	return s.reconnect && (s.maxRetries <= 0 || retries < s.maxRetries)
}

// handleSSEStream 处理SSE流数据
func (s *SSERequest) handleSSEStream(stream io.Reader) error {
	scanner := bufio.NewScanner(stream)

	var currentEvent SSEEvent

	for scanner.Scan() {
		line := scanner.Text()

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
			// 解析重试时间
			fmt.Sscanf(value, "%d", &currentEvent.Retry)
		}
	}

	return scanner.Err()
}

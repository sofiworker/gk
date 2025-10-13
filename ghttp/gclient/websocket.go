package gclient

import (
	"context"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketMessage WebSocket消息
type WebSocketMessage struct {
	Type int
	Data []byte
}

// WebSocketHandler WebSocket消息处理器
type WebSocketHandler func(message WebSocketMessage) error

// WebSocketRequest WebSocket请求配置
type WebSocketRequest struct {
	*Request
	dialer          *websocket.Dialer
	handler         WebSocketHandler
	reconnect       bool
	retryDelay      time.Duration
	maxRetries      int
	readBufferSize  int
	writeBufferSize int
	subprotocols    []string
}

func NewWebSocketRequest(r *Request) *WebSocketRequest {
	return &WebSocketRequest{Request: r}
}

// SetWebSocketHandler 设置WebSocket消息处理器
func (r *Request) SetWebSocketHandler(handler WebSocketHandler) *WebSocketRequest {
	wsReq := &WebSocketRequest{
		Request: r,
		dialer: &websocket.Dialer{
			HandshakeTimeout: 45 * time.Second,
		},
		handler:         handler,
		reconnect:       true,
		retryDelay:      3 * time.Second,
		maxRetries:      5,
		readBufferSize:  1024,
		writeBufferSize: 1024,
	}
	return wsReq
}

// SetReconnect 设置是否自动重连
func (w *WebSocketRequest) SetReconnect(reconnect bool) *WebSocketRequest {
	w.reconnect = reconnect
	return w
}

// SetRetryDelay 设置重试延迟
func (w *WebSocketRequest) SetRetryDelay(delay time.Duration) *WebSocketRequest {
	w.retryDelay = delay
	return w
}

// SetMaxRetries 设置最大重试次数
func (w *WebSocketRequest) SetMaxRetries(maxRetries int) *WebSocketRequest {
	w.maxRetries = maxRetries
	return w
}

// SetReadBufferSize 设置读缓冲区大小
func (w *WebSocketRequest) SetReadBufferSize(size int) *WebSocketRequest {
	w.readBufferSize = size
	if w.dialer != nil {
		w.dialer.ReadBufferSize = size
	}
	return w
}

// SetWriteBufferSize 设置写缓冲区大小
func (w *WebSocketRequest) SetWriteBufferSize(size int) *WebSocketRequest {
	w.writeBufferSize = size
	if w.dialer != nil {
		w.dialer.WriteBufferSize = size
	}
	return w
}

// SetSubprotocols 设置子协议
func (w *WebSocketRequest) SetSubprotocols(subprotocols []string) *WebSocketRequest {
	w.subprotocols = subprotocols
	if w.dialer != nil {
		w.dialer.Subprotocols = subprotocols
	}
	return w
}

// Connect 连接WebSocket服务器
func (w *WebSocketRequest) Connect(ctx context.Context) error {

	return nil
}

// shouldReconnect 判断是否应该重连
func (w *WebSocketRequest) shouldReconnect(retries int, err error) bool {
	return w.reconnect && (w.maxRetries <= 0 || retries < w.maxRetries)
}

// handleWebSocketConnection 处理WebSocket连接
func (w *WebSocketRequest) handleWebSocketConnection(ctx context.Context, conn *websocket.Conn) error {
	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 读取消息
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		// 调用处理器
		if w.handler != nil {
			err = w.handler(WebSocketMessage{
				Type: messageType,
				Data: data,
			})
			if err != nil {
				return err
			}
		}

		// 重置读取超时
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	}
}

// SendMessage 发送消息
func (w *WebSocketRequest) SendMessage(messageType int, data []byte) error {
	// 这个方法需要在连接建立后才能使用
	// 可以考虑添加连接状态管理
	return nil
}

package gclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
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

	conn   *websocket.Conn
	connMu sync.Mutex
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
	if w.handler == nil {
		return fmt.Errorf("websocket handler is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if w.dialer == nil {
		w.dialer = &websocket.Dialer{HandshakeTimeout: 45 * time.Second}
	}
	w.dialer.ReadBufferSize = w.readBufferSize
	w.dialer.WriteBufferSize = w.writeBufferSize
	if len(w.subprotocols) > 0 {
		w.dialer.Subprotocols = append([]string(nil), w.subprotocols...)
	}

	retries := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := w.Request.Clone()
		req.SetContext(ctx)
		fullURL, err := req.prepareURL()
		if err != nil {
			return err
		}

		wsURL, err := toWebSocketURL(fullURL)
		if err != nil {
			return err
		}

		header := http.Header{}
		for k, v := range req.Header {
			header[k] = append([]string(nil), v...)
		}
		for _, ck := range req.Cookies {
			if ck != nil {
				header.Add("Cookie", ck.String())
			}
		}

		conn, resp, err := w.dialer.DialContext(ctx, wsURL, header)
		if err != nil {
			if resp != nil {
				_ = resp.Body.Close()
			}
			if !w.shouldReconnect(retries, err) {
				return err
			}
			retries++
			if err := sleepWithContext(ctx, w.retryDelay); err != nil {
				return err
			}
			continue
		}

		w.setConn(conn)
		err = w.handleWebSocketConnection(ctx, conn)
		w.setConn(nil)
		_ = conn.Close()

		if err == nil {
			return nil
		}
		if !w.shouldReconnect(retries, err) {
			return err
		}
		retries++
		if err := sleepWithContext(ctx, w.retryDelay); err != nil {
			return err
		}
	}
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
	w.connMu.Lock()
	defer w.connMu.Unlock()
	if w.conn == nil {
		return fmt.Errorf("websocket connection not established")
	}
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return w.conn.WriteMessage(messageType, data)
}

func (w *WebSocketRequest) setConn(conn *websocket.Conn) {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	w.conn = conn
}

func toWebSocketURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported scheme %s", parsed.Scheme)
	}
	return parsed.String(), nil
}

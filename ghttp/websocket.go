package ghttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	gwebsocket "github.com/gorilla/websocket"
)

var ErrWebSocketConnNotInitialized = errors.New("websocket connection is not initialized")

// WebSocketMessageType 标识 WebSocket 数据帧类型。
// WebSocketMessageType identifies a WebSocket data frame type.
type WebSocketMessageType int

const (
	TextMessage   WebSocketMessageType = 1
	BinaryMessage WebSocketMessageType = 2
)

// WebSocketConn 包装 WebSocket 连接并提供 JSON 辅助。
// WebSocketConn wraps a WebSocket connection with JSON helpers.
type WebSocketConn struct {
	conn webSocketConn
}

type webSocketConn interface {
	ReadJSON(v interface{}) error
	WriteJSON(v interface{}) error
	ReadMessage() (int, []byte, error)
	WriteMessage(messageType int, data []byte) error
	Close() error
	Subprotocol() string
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
	WriteControl(messageType int, data []byte, deadline time.Time) error
}

func (c *WebSocketConn) ReadJSON(v interface{}) error {
	if c == nil || c.conn == nil {
		return ErrWebSocketConnNotInitialized
	}
	return c.conn.ReadJSON(v)
}

func (c *WebSocketConn) WriteJSON(v interface{}) error {
	if c == nil || c.conn == nil {
		return ErrWebSocketConnNotInitialized
	}
	return c.conn.WriteJSON(v)
}

func (c *WebSocketConn) Close() error {
	if c == nil || c.conn == nil {
		return ErrWebSocketConnNotInitialized
	}
	return c.conn.Close()
}

// ReadMessage 读取下一条原始 WebSocket 帧。
// ReadMessage reads the next raw WebSocket frame.
func (c *WebSocketConn) ReadMessage() (WebSocketMessageType, []byte, error) {
	if c == nil || c.conn == nil {
		return 0, nil, ErrWebSocketConnNotInitialized
	}
	messageType, data, err := c.conn.ReadMessage()
	return WebSocketMessageType(messageType), data, err
}

// WriteMessage 写入原始 WebSocket 帧。
// WriteMessage writes a raw WebSocket frame.
func (c *WebSocketConn) WriteMessage(messageType WebSocketMessageType, data []byte) error {
	if c == nil || c.conn == nil {
		return ErrWebSocketConnNotInitialized
	}
	return c.conn.WriteMessage(int(messageType), data)
}

// Subprotocol 返回握手协商的子协议，未协商时返回空字符串。
// Subprotocol returns the negotiated subprotocol, or empty.
func (c *WebSocketConn) Subprotocol() string {
	if c == nil || c.conn == nil {
		return ""
	}
	return c.conn.Subprotocol()
}

// SetReadDeadline 设置底层连接的读截止时间。
// SetReadDeadline sets the read deadline.
func (c *WebSocketConn) SetReadDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return ErrWebSocketConnNotInitialized
	}
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline 设置底层连接的写截止时间。
// SetWriteDeadline sets the write deadline.
func (c *WebSocketConn) SetWriteDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return ErrWebSocketConnNotInitialized
	}
	return c.conn.SetWriteDeadline(t)
}

// ReadJSONContext 读取一条 JSON 消息，context 结束时返回 ctx.Err()。
// ReadJSONContext reads one JSON message, returning ctx.Err() on cancel.
// 阻塞读通过读截止时间解除；取消后连接视为已关闭。
// the blocking read is unblocked by a deadline; treat the conn as closed after.
func (c *WebSocketConn) ReadJSONContext(ctx context.Context, v interface{}) error {
	if c == nil || c.conn == nil {
		return ErrWebSocketConnNotInitialized
	}
	if ctx == nil {
		return c.ReadJSON(v)
	}
	done := make(chan error, 1)
	go func() {
		done <- c.ReadJSON(v)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = c.conn.SetReadDeadline(time.Now())
		<-done
		return ctx.Err()
	}
}

// WriteJSONContext 写入一条 JSON 消息，context 结束时返回 ctx.Err()。
// WriteJSONContext writes one JSON message, returning ctx.Err() on cancel.
// 阻塞写通过写截止时间解除；取消后连接视为已关闭。
// the blocking write is unblocked by a deadline; treat the conn as closed after.
func (c *WebSocketConn) WriteJSONContext(ctx context.Context, v interface{}) error {
	if c == nil || c.conn == nil {
		return ErrWebSocketConnNotInitialized
	}
	if ctx == nil {
		return c.WriteJSON(v)
	}
	done := make(chan error, 1)
	go func() {
		done <- c.WriteJSON(v)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = c.conn.SetWriteDeadline(time.Now())
		<-done
		return ctx.Err()
	}
}

// WebSocketHandler 是 WebSocket 升级的 handler 类型。
// WebSocketHandler is the handler type for WebSocket upgrades.
type WebSocketHandler func(ctx context.Context, params Params, conn *WebSocketConn) error

func webSocketSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host
}

func buildWebSocketHandler(s *Server, handler WebSocketHandler, routeCheckOrigin func(*http.Request) bool) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, routeParams pathParamList) {
		checkOrigin := s.config.webSocketCheckOrigin
		if routeCheckOrigin != nil {
			checkOrigin = routeCheckOrigin
		}
		if checkOrigin == nil {
			checkOrigin = webSocketSameOrigin
		}
		upgrader := gwebsocket.Upgrader{
			CheckOrigin:     checkOrigin,
			Subprotocols:    s.config.webSocketSubprotocols,
			ReadBufferSize:  s.config.webSocketReadBufferSize,
			WriteBufferSize: s.config.webSocketWriteBufferSize,
		}
		params := paramsFromRequestWithPathParams(r, s.config, routeParams)
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			if s.logger != nil {
				s.logger.WarnContext(r.Context(), "websocket upgrade failed", "error", err, "path", r.URL.Path)
			}
			return
		}

		conn := &WebSocketConn{conn: ws}
		defer conn.Close()
		stopKeepAlive := startWebSocketKeepAlive(conn, s.config.webSocketPingPeriod, s.config.webSocketPongWait)
		defer stopKeepAlive()
		if err := handler(r.Context(), params, conn); err != nil {
			if s.logger != nil {
				s.logger.ErrorContext(r.Context(), "websocket handler error", "error", err, "path", r.URL.Path)
			}
		}
	})
}

// startWebSocketKeepAlive 周期发送 ping 并在 pong 时刷新读截止时间。
// startWebSocketKeepAlive sends pings and refreshes the read deadline on pongs.
// 需显式开启：两个周期都必须为正，否则返回空操作。
// opt-in: both periods must be positive, otherwise it is a no-op.
func startWebSocketKeepAlive(c *WebSocketConn, pingPeriod, pongWait time.Duration) func() {
	if c == nil || c.conn == nil || pingPeriod <= 0 || pongWait <= 0 {
		return func() {}
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := c.conn.WriteControl(gwebsocket.PingMessage, nil, time.Now().Add(pongWait)); err != nil {
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

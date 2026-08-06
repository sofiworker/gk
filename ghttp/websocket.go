package ghttp

import (
	"context"
	"errors"
	"net/http"

	gwebsocket "github.com/gorilla/websocket"
)

var ErrWebSocketConnNotInitialized = errors.New("websocket connection is not initialized")

// WebSocketConn wraps a WebSocket connection with JSON helpers.
type WebSocketConn struct {
	conn webSocketJSONConn
}

type webSocketJSONConn interface {
	ReadJSON(v interface{}) error
	WriteJSON(v interface{}) error
	Close() error
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

func buildWebSocketHandler(s *Server, handler WebSocketHandler) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, routeParams pathParamList) {
		checkOrigin := s.config.webSocketCheckOrigin
		if checkOrigin == nil {
			checkOrigin = webSocketSameOrigin
		}
		upgrader := gwebsocket.Upgrader{
			CheckOrigin: checkOrigin,
		}
		params := paramsFromRequestWithPathParams(r, s.config, routeParams)
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		conn := &WebSocketConn{conn: ws}
		defer conn.Close()
		_ = handler(r.Context(), params, conn)
	})
}

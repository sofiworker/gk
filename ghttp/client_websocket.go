package ghttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	gwebsocket "github.com/gorilla/websocket"
)

func (c *Client) WebSocket(rawURL string, opts ...WebSocketOption) (*WebSocketConn, error) {
	cfg := &clientWebSocketConfig{
		handshakeTimeout: 10 * time.Second,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	dialer := gwebsocket.Dialer{
		TLSClientConfig:  cfg.tlsConfig,
		HandshakeTimeout: cfg.handshakeTimeout,
		ReadBufferSize:   cfg.readBufferSize,
		WriteBufferSize:  cfg.writeBufferSize,
		Subprotocols:     append([]string(nil), cfg.subprotocols...),
	}

	ctx := context.Background()
	cancel := func() {}
	if cfg.handshakeTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, cfg.handshakeTimeout)
	}
	defer cancel()

	ws, _, err := dialer.DialContext(ctx, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial %s: %w", rawURL, err)
	}
	return &WebSocketConn{conn: ws}, nil
}

type clientWebSocketConfig struct {
	tlsConfig        *tls.Config
	subprotocols     []string
	handshakeTimeout time.Duration
	readBufferSize   int
	writeBufferSize  int
}

type WebSocketOption func(*clientWebSocketConfig)

func WithWebSocketTLS(tlsConfig *tls.Config) WebSocketOption {
	return func(c *clientWebSocketConfig) { c.tlsConfig = tlsConfig }
}

func WithWebSocketSubprotocols(protos []string) WebSocketOption {
	return func(c *clientWebSocketConfig) { c.subprotocols = append([]string(nil), protos...) }
}

func WithWebSocketHandshakeTimeout(timeout time.Duration) WebSocketOption {
	return func(c *clientWebSocketConfig) { c.handshakeTimeout = timeout }
}

func WithWebSocketReadBufferSize(size int) WebSocketOption {
	return func(c *clientWebSocketConfig) { c.readBufferSize = size }
}

func WithWebSocketWriteBufferSize(size int) WebSocketOption {
	return func(c *clientWebSocketConfig) { c.writeBufferSize = size }
}

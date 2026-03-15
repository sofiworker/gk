package gclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WebSocketMessage struct {
	Type int
	Data []byte
}

func (m WebSocketMessage) DecodeJSON(target interface{}) error {
	if target == nil {
		return nil
	}
	return json.Unmarshal(m.Data, target)
}

type WebSocketHandler func(message WebSocketMessage) error

type WebSocketRequest struct {
	*Request
	dialer           *websocket.Dialer
	handler          WebSocketHandler
	onConnect        func(*websocket.Conn, *http.Response) error
	onClose          func(error)
	onRetry          func(int, error, time.Duration)
	onError          func(error)
	observer         StreamObserver
	beforeWrite      func(int, []byte) error
	reconnect        bool
	retryDelay       time.Duration
	maxRetries       int
	readBufferSize   int
	writeBufferSize  int
	subprotocols     []string
	handshakeTimeout time.Duration
	pingInterval     time.Duration
	pongWait         time.Duration
	writeWait        time.Duration

	conn   *websocket.Conn
	connMu sync.Mutex
}

func NewWebSocketRequest(r *Request) *WebSocketRequest {
	if r == nil {
		r = NewClient().R()
	}
	return &WebSocketRequest{
		Request:          r,
		reconnect:        true,
		retryDelay:       3 * time.Second,
		maxRetries:       5,
		readBufferSize:   1024,
		writeBufferSize:  1024,
		handshakeTimeout: 45 * time.Second,
		pongWait:         60 * time.Second,
		writeWait:        10 * time.Second,
	}
}

func (c *Client) NewWebSocketRequest() *WebSocketRequest {
	return NewWebSocketRequest(c.R())
}

func (r *Request) NewWebSocketRequest() *WebSocketRequest {
	return NewWebSocketRequest(r)
}

func (r *Request) SetWebSocketHandler(handler WebSocketHandler) *WebSocketRequest {
	return NewWebSocketRequest(r).SetHandler(handler)
}

func (w *WebSocketRequest) SetHandler(handler WebSocketHandler) *WebSocketRequest {
	w.handler = handler
	return w
}

func (w *WebSocketRequest) SetJSONHandler(factory func() interface{}, handler func(interface{}) error) *WebSocketRequest {
	if factory == nil {
		return w.SetHandler(func(WebSocketMessage) error {
			return errors.New("json message factory is required")
		})
	}
	return w.SetHandler(func(message WebSocketMessage) error {
		target := factory()
		if err := message.DecodeJSON(target); err != nil {
			return err
		}
		if handler != nil {
			return handler(target)
		}
		return nil
	})
}

func (w *WebSocketRequest) SetDialer(dialer *websocket.Dialer) *WebSocketRequest {
	w.dialer = dialer
	return w
}

func (w *WebSocketRequest) OnConnect(fn func(*websocket.Conn, *http.Response) error) *WebSocketRequest {
	w.onConnect = fn
	return w
}

func (w *WebSocketRequest) OnClose(fn func(error)) *WebSocketRequest {
	w.onClose = fn
	return w
}

func (w *WebSocketRequest) OnRetry(fn func(int, error, time.Duration)) *WebSocketRequest {
	w.onRetry = fn
	return w
}

func (w *WebSocketRequest) OnError(fn func(error)) *WebSocketRequest {
	w.onError = fn
	return w
}

func (w *WebSocketRequest) SetObserver(observer StreamObserver) *WebSocketRequest {
	w.observer = observer
	return w
}

func (w *WebSocketRequest) BeforeWrite(fn func(int, []byte) error) *WebSocketRequest {
	w.beforeWrite = fn
	return w
}

func (w *WebSocketRequest) SetReconnect(reconnect bool) *WebSocketRequest {
	w.reconnect = reconnect
	return w
}

func (w *WebSocketRequest) SetRetryDelay(delay time.Duration) *WebSocketRequest {
	w.retryDelay = delay
	return w
}

func (w *WebSocketRequest) SetMaxRetries(maxRetries int) *WebSocketRequest {
	w.maxRetries = maxRetries
	return w
}

func (w *WebSocketRequest) SetReadBufferSize(size int) *WebSocketRequest {
	w.readBufferSize = size
	if w.dialer != nil {
		w.dialer.ReadBufferSize = size
	}
	return w
}

func (w *WebSocketRequest) SetWriteBufferSize(size int) *WebSocketRequest {
	w.writeBufferSize = size
	if w.dialer != nil {
		w.dialer.WriteBufferSize = size
	}
	return w
}

func (w *WebSocketRequest) SetSubprotocols(subprotocols []string) *WebSocketRequest {
	w.subprotocols = append([]string(nil), subprotocols...)
	if w.dialer != nil {
		w.dialer.Subprotocols = append([]string(nil), subprotocols...)
	}
	return w
}

func (w *WebSocketRequest) SetHandshakeTimeout(timeout time.Duration) *WebSocketRequest {
	w.handshakeTimeout = timeout
	if w.dialer != nil {
		w.dialer.HandshakeTimeout = timeout
	}
	return w
}

func (w *WebSocketRequest) SetPingInterval(interval time.Duration) *WebSocketRequest {
	w.pingInterval = interval
	return w
}

func (w *WebSocketRequest) SetPongWait(wait time.Duration) *WebSocketRequest {
	w.pongWait = wait
	return w
}

func (w *WebSocketRequest) SetWriteWait(wait time.Duration) *WebSocketRequest {
	w.writeWait = wait
	return w
}

func (w *WebSocketRequest) Dial(ctx context.Context) (*websocket.Conn, *http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	req := w.Request.Clone()
	req.SetMethod(http.MethodGet)
	req.SetContext(ctx)
	httpReq, err := req.BuildHTTPRequest()
	if err != nil {
		return nil, nil, err
	}

	wsURL, err := toWebSocketURL(httpReq.URL.String())
	if err != nil {
		return nil, nil, err
	}

	header := httpReq.Header.Clone()
	dialer := w.buildDialer(req)

	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, resp, err
	}
	w.setConn(conn)
	if w.onConnect != nil {
		if err := w.onConnect(conn, resp); err != nil {
			_ = conn.Close()
			w.setConn(nil)
			return nil, resp, err
		}
	}
	if w.observer != nil {
		if err := w.observer.OnConnect(StreamConnectInfo{
			Protocol: StreamProtocolWebSocket,
			URL:      req.URL,
			Response: resp,
		}); err != nil {
			_ = conn.Close()
			w.setConn(nil)
			return nil, resp, err
		}
	}
	return conn, resp, nil
}

func (w *WebSocketRequest) Connect(ctx context.Context) error {
	if w.handler == nil {
		return errors.New("websocket handler is required")
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

		conn, resp, err := w.Dial(ctx)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			w.callOnError(err)
			if !w.shouldReconnect(retries, err) {
				return err
			}
			w.callOnRetry(retries+1, err)
			retries++
			if err := sleepWithContext(ctx, w.retryDelay); err != nil {
				return err
			}
			continue
		}

		err = w.handleWebSocketConnection(ctx, conn)
		w.setConn(nil)
		_ = conn.Close()
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			w.callOnError(err)
		}
		if w.onClose != nil {
			w.onClose(err)
		}
		if w.observer != nil {
			w.observer.OnClose(StreamCloseInfo{
				Protocol: StreamProtocolWebSocket,
				URL:      w.Request.URL,
				Err:      err,
			})
		}

		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if !w.shouldReconnect(retries, err) {
			return err
		}
		w.callOnRetry(retries+1, err)
		retries++
		if err := sleepWithContext(ctx, w.retryDelay); err != nil {
			return err
		}
	}
}

func (w *WebSocketRequest) buildDialer(req *Request) *websocket.Dialer {
	if w.dialer != nil {
		dialer := *w.dialer
		if w.handshakeTimeout > 0 {
			dialer.HandshakeTimeout = w.handshakeTimeout
		}
		if w.readBufferSize > 0 {
			dialer.ReadBufferSize = w.readBufferSize
		}
		if w.writeBufferSize > 0 {
			dialer.WriteBufferSize = w.writeBufferSize
		}
		if len(w.subprotocols) > 0 {
			dialer.Subprotocols = append([]string(nil), w.subprotocols...)
		}
		applyRequestDialerProxy(&dialer, req)
		applyClientDialerConfig(&dialer, req.effectiveClient())
		return &dialer
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: w.handshakeTimeout,
		ReadBufferSize:   w.readBufferSize,
		WriteBufferSize:  w.writeBufferSize,
		Subprotocols:     append([]string(nil), w.subprotocols...),
	}
	applyClientDialerConfig(dialer, req.effectiveClient())
	applyRequestDialerProxy(dialer, req)
	return dialer
}

func applyClientDialerConfig(dialer *websocket.Dialer, client *Client) {
	if dialer == nil || client == nil || client.config == nil {
		return
	}
	if client.config.TLSConfig != nil {
		dialer.TLSClientConfig = cloneTLSConfig(client.config.TLSConfig)
	}
	if client.config.ProxyConfig != nil {
		switch {
		case client.config.ProxyConfig.NoProxy:
			dialer.Proxy = nil
		case client.config.ProxyConfig.ProxyFunc != nil:
			dialer.Proxy = client.config.ProxyConfig.ProxyFunc
		case client.config.ProxyConfig.URL != "":
			if parsed, err := url.Parse(client.config.ProxyConfig.URL); err == nil {
				dialer.Proxy = http.ProxyURL(parsed)
			}
		}
	}
}

func applyRequestDialerProxy(dialer *websocket.Dialer, req *Request) {
	if dialer == nil || req == nil {
		return
	}
	switch {
	case req.disableProxy:
		dialer.Proxy = nil
	case req.proxyFunc != nil:
		dialer.Proxy = req.proxyFunc
	case req.proxyURL != "":
		if parsed, err := url.Parse(req.proxyURL); err == nil {
			dialer.Proxy = http.ProxyURL(parsed)
		}
	}
}

func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return nil
	}
	return cfg.Clone()
}

func (w *WebSocketRequest) shouldReconnect(retries int, _ error) bool {
	return w.reconnect && (w.maxRetries <= 0 || retries < w.maxRetries)
}

func (w *WebSocketRequest) handleWebSocketConnection(ctx context.Context, conn *websocket.Conn) error {
	pongWait := w.pongWait
	if pongWait <= 0 {
		pongWait = 60 * time.Second
	}
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	done := make(chan struct{})
	if w.pingInterval > 0 {
		go w.startHeartbeat(ctx, conn, done)
	}
	defer close(done)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return normalizeWebSocketCloseError(err)
		}
		if w.handler != nil {
			if err := w.handler(WebSocketMessage{Type: messageType, Data: data}); err != nil {
				return err
			}
		}
		conn.SetReadDeadline(time.Now().Add(pongWait))
	}
}

func (w *WebSocketRequest) startHeartbeat(ctx context.Context, conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(w.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			_ = w.writeControl(conn, websocket.PingMessage, nil)
		}
	}
}

func (w *WebSocketRequest) writeControl(conn *websocket.Conn, messageType int, data []byte) error {
	writeWait := w.writeWait
	if writeWait <= 0 {
		writeWait = 10 * time.Second
	}
	return conn.WriteControl(messageType, data, time.Now().Add(writeWait))
}

func (w *WebSocketRequest) SendMessage(messageType int, data []byte) error {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	if w.conn == nil {
		return errors.New("websocket connection not established")
	}
	if w.beforeWrite != nil {
		if err := w.beforeWrite(messageType, data); err != nil {
			return err
		}
	}
	writeWait := w.writeWait
	if writeWait <= 0 {
		writeWait = 10 * time.Second
	}
	w.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return w.conn.WriteMessage(messageType, data)
}

func (w *WebSocketRequest) WriteText(text string) error {
	return w.SendMessage(websocket.TextMessage, []byte(text))
}

func (w *WebSocketRequest) WriteBinary(data []byte) error {
	return w.SendMessage(websocket.BinaryMessage, data)
}

func (w *WebSocketRequest) WriteJSON(v interface{}) error {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	if w.conn == nil {
		return errors.New("websocket connection not established")
	}
	writeWait := w.writeWait
	if writeWait <= 0 {
		writeWait = 10 * time.Second
	}
	w.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return w.conn.WriteJSON(v)
}

func (w *WebSocketRequest) ReadMessage() (WebSocketMessage, error) {
	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()
	if conn == nil {
		return WebSocketMessage{}, errors.New("websocket connection not established")
	}
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		return WebSocketMessage{}, normalizeWebSocketCloseError(err)
	}
	return WebSocketMessage{Type: messageType, Data: data}, nil
}

func (w *WebSocketRequest) ReadJSON(target interface{}) error {
	msg, err := w.ReadMessage()
	if err != nil {
		return err
	}
	return json.Unmarshal(msg.Data, target)
}

func (w *WebSocketRequest) Ping() error {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	if w.conn == nil {
		return errors.New("websocket connection not established")
	}
	return w.writeControl(w.conn, websocket.PingMessage, nil)
}

func (w *WebSocketRequest) WritePong(data []byte) error {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	if w.conn == nil {
		return errors.New("websocket connection not established")
	}
	return w.writeControl(w.conn, websocket.PongMessage, data)
}

func (w *WebSocketRequest) Close() error {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	if w.conn == nil {
		return nil
	}
	err := w.conn.Close()
	w.conn = nil
	return err
}

func (w *WebSocketRequest) Conn() *websocket.Conn {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	return w.conn
}

func (w *WebSocketRequest) setConn(conn *websocket.Conn) {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	w.conn = conn
}

func (w *WebSocketRequest) callOnRetry(attempt int, err error) {
	if w.onRetry != nil {
		w.onRetry(attempt, err, w.retryDelay)
	}
	if w.observer != nil {
		w.observer.OnRetry(StreamRetryInfo{
			Protocol: StreamProtocolWebSocket,
			URL:      w.Request.URL,
			Attempt:  attempt,
			Delay:    w.retryDelay,
			Err:      err,
		})
	}
}

func (w *WebSocketRequest) callOnError(err error) {
	if err != nil && w.onError != nil {
		w.onError(err)
	}
	if err != nil && w.observer != nil {
		w.observer.OnError(StreamProtocolWebSocket, w.Request.URL, err)
	}
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

func normalizeWebSocketCloseError(err error) error {
	if err == nil {
		return nil
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return &WebSocketCloseError{
			Code:   closeErr.Code,
			Reason: closeErr.Text,
			Err:    err,
		}
	}
	return err
}

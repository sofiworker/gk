package ws

import (
	"bufio"
	"bytes"
	"crypto/sha1" //nolint:gosec // RFC 6455 规定使用 SHA-1 / mandated by RFC 6455
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// acceptGUID 是 RFC 6455 §1.3 定义的固定 GUID。
// acceptGUID is the fixed GUID defined in RFC 6455 §1.3.
const acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// 握手相关请求/响应头名称。
// Handshake request/response header names.
const (
	headerKey       = "Sec-Websocket-Key"
	headerVersion   = "Sec-Websocket-Version"
	headerProtocol  = "Sec-Websocket-Protocol"
	headerAccept    = "Sec-Websocket-Accept"
	headerExtension = "Sec-Websocket-Extensions"
)

// AcceptKey 按 RFC 6455 由 Sec-WebSocket-Key 计算 Sec-WebSocket-Accept。
// AcceptKey computes Sec-WebSocket-Accept from Sec-WebSocket-Key per RFC 6455.
func AcceptKey(key string) string {
	h := sha1.New() //nolint:gosec // RFC 6455 规定使用 SHA-1 / mandated by RFC 6455
	_, _ = io.WriteString(h, key)
	_, _ = io.WriteString(h, acceptGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// IsWebSocketUpgrade 报告 r 是否声明了 WebSocket 升级（Connection 含 upgrade 且
// Upgrade 含 websocket）。可供中间件跳过压缩、超时等对升级请求不适用的处理。
// IsWebSocketUpgrade reports whether r asks for a WebSocket upgrade (Connection contains
// upgrade and Upgrade contains websocket). Middleware can use it to skip compression,
// timeouts and similar handling that does not apply to upgrades.
func IsWebSocketUpgrade(r *http.Request) bool {
	return headerContainsToken(r.Header, "Connection", "upgrade") &&
		headerContainsToken(r.Header, "Upgrade", "websocket")
}

// Subprotocols 返回客户端在 Sec-WebSocket-Protocol 中提供的子协议列表。
// Subprotocols returns the subprotocols offered by the client in Sec-WebSocket-Protocol.
func Subprotocols(r *http.Request) []string {
	var out []string
	for _, v := range r.Header.Values(headerProtocol) {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// SameOrigin 是默认的 Origin 校验：Origin 缺失时放行（非浏览器客户端），否则要求
// Origin 的 host 与请求 Host 一致（忽略大小写）。
// SameOrigin is the default Origin check: requests without Origin pass (non-browser
// clients); otherwise the Origin host must equal the request Host (case-insensitive).
func SameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// Upgrade 校验 WebSocket 握手请求并把 HTTP 连接升级为 *Conn。
//
// 握手失败时 Upgrade 已经写出 4xx 响应（方法不对 405、头不合法 400、版本不支持 426、
// Origin 被拒 403），返回 *HandshakeError（解包为 ErrBadHandshake 或 ErrOriginNotAllowed），
// 调用方不应再写响应。连接无法接管（如 HTTP/2）时写出 500 并返回包装后的 Hijack 错误。
// 升级成功后 w 不可再使用。
//
// Upgrade validates a WebSocket handshake request and upgrades the HTTP connection to a
// *Conn.
//
// On handshake failure Upgrade has already written a 4xx response (405 for the method,
// 400 for invalid headers, 426 for an unsupported version, 403 for a rejected Origin)
// and returns a *HandshakeError (unwrapping to ErrBadHandshake or ErrOriginNotAllowed);
// the caller must not write another response. When the connection cannot be hijacked
// (e.g. HTTP/2) it writes 500 and returns the wrapped Hijack error. After a successful
// upgrade w must not be used.
func Upgrade(w http.ResponseWriter, r *http.Request, opts ...Option) (*Conn, error) {
	cfg := newConfig(opts)

	key, herr := checkHandshake(r, cfg)
	if herr != nil {
		switch herr.Status {
		case http.StatusMethodNotAllowed:
			w.Header().Set("Allow", http.MethodGet)
		case http.StatusBadRequest, http.StatusUpgradeRequired:
			w.Header().Set(headerVersion, SupportedVersion)
		}
		cfg.errorHandler(w, r, herr.Status, herr)
		return nil, herr
	}

	protocol := selectSubprotocol(cfg.subprotocols, Subprotocols(r))

	netConn, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, fmt.Errorf("ws: hijack: %w", err)
	}

	// 清除 http.Server 设置的读写超时，连接从此由 Conn 管理。
	// Clear the deadlines set by http.Server; the connection is managed by Conn from now on.
	_ = netConn.SetDeadline(time.Time{})

	var src io.Reader = netConn
	if n := brw.Reader.Buffered(); n > 0 {
		// 客户端可能在握手后立即发送帧，这些字节已被 net/http 读入缓冲。
		// The client may send frames right after the handshake; net/http already
		// buffered those bytes.
		p, _ := brw.Reader.Peek(n)
		src = &prefixReader{prefix: bytes.Clone(p), r: netConn}
	}
	br := bufio.NewReaderSize(src, cfg.readBufferSize)
	bw := bufio.NewWriterSize(netConn, cfg.writeBufferSize)

	if cfg.handshakeTimeout > 0 {
		_ = netConn.SetWriteDeadline(time.Now().Add(cfg.handshakeTimeout))
	}
	writeHandshakeResponse(bw, AcceptKey(key), protocol, cfg.responseHeader)
	if err := bw.Flush(); err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("ws: write handshake response: %w", err)
	}
	if cfg.handshakeTimeout > 0 {
		_ = netConn.SetWriteDeadline(time.Time{})
	}

	return newConn(netConn, br, bw, true, protocol, cfg), nil
}

// checkHandshake 校验握手请求，成功时返回 Sec-WebSocket-Key。
// checkHandshake validates the handshake request and returns Sec-WebSocket-Key.
func checkHandshake(r *http.Request, cfg *config) (string, *HandshakeError) {
	bad := func(status int, reason string) *HandshakeError {
		return &HandshakeError{Status: status, Reason: reason, kind: ErrBadHandshake}
	}
	if r.Method != http.MethodGet {
		return "", bad(http.StatusMethodNotAllowed, "method is not GET")
	}
	if !r.ProtoAtLeast(1, 1) {
		return "", bad(http.StatusBadRequest, "HTTP/1.1 or later required")
	}
	if !headerContainsToken(r.Header, "Connection", "upgrade") {
		return "", bad(http.StatusBadRequest, "'Connection' header does not contain 'upgrade'")
	}
	if !headerContainsToken(r.Header, "Upgrade", "websocket") {
		return "", bad(http.StatusBadRequest, "'Upgrade' header does not contain 'websocket'")
	}
	if v := r.Header.Get(headerVersion); strings.TrimSpace(v) != SupportedVersion {
		return "", bad(http.StatusUpgradeRequired, "unsupported 'Sec-WebSocket-Version'")
	}
	key := strings.TrimSpace(r.Header.Get(headerKey))
	if !validKey(key) {
		return "", bad(http.StatusBadRequest, "invalid 'Sec-WebSocket-Key'")
	}
	if !cfg.checkOrigin(r) {
		return "", &HandshakeError{Status: http.StatusForbidden, Reason: "origin not allowed", kind: ErrOriginNotAllowed}
	}
	return key, nil
}

// validKey 报告 key 是否为 base64 编码的 16 字节值。
// validKey reports whether key is a base64-encoded 16-byte value.
func validKey(key string) bool {
	if key == "" {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(b) == 16
}

// headerContainsToken 报告头 name 的逗号分隔值中是否含有 token（忽略大小写）。
// headerContainsToken reports whether the comma-separated values of header name contain
// token (case-insensitive).
func headerContainsToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// selectSubprotocol 按服务端偏好返回第一个客户端也提供的子协议；无匹配时返回空串。
// selectSubprotocol returns the first server-preferred subprotocol also offered by the
// client, or "" when none matches.
func selectSubprotocol(server, client []string) string {
	for _, s := range server {
		for _, c := range client {
			if s == c {
				return s
			}
		}
	}
	return ""
}

// writeHandshakeResponse 写出 101 响应。extra 中的握手头与非法头被跳过。
// writeHandshakeResponse writes the 101 response. Handshake headers and invalid headers
// in extra are skipped.
func writeHandshakeResponse(bw *bufio.Writer, accept, protocol string, extra http.Header) {
	_, _ = bw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ")
	_, _ = bw.WriteString(accept)
	_, _ = bw.WriteString("\r\n")
	if protocol != "" {
		_, _ = bw.WriteString("Sec-WebSocket-Protocol: ")
		_, _ = bw.WriteString(protocol)
		_, _ = bw.WriteString("\r\n")
	}
	for name, values := range extra {
		if skipExtraHeader(name) {
			continue
		}
		for _, v := range values {
			if strings.ContainsAny(v, "\r\n") {
				continue
			}
			_, _ = bw.WriteString(name)
			_, _ = bw.WriteString(": ")
			_, _ = bw.WriteString(v)
			_, _ = bw.WriteString("\r\n")
		}
	}
	_, _ = bw.WriteString("\r\n")
}

// skipExtraHeader 报告额外响应头 name 是否必须跳过（握手头或含非法字符）。
// skipExtraHeader reports whether extra response header name must be skipped
// (handshake header or invalid characters).
func skipExtraHeader(name string) bool {
	if name == "" || strings.ContainsAny(name, " :\r\n") {
		return true
	}
	switch http.CanonicalHeaderKey(name) {
	case "Upgrade", "Connection", headerAccept, headerProtocol, headerExtension:
		return true
	}
	return false
}

// prefixReader 先返回握手时已缓冲的字节，再从连接读取。
// prefixReader returns the bytes buffered during the handshake first, then reads from
// the connection.
type prefixReader struct {
	prefix []byte
	r      net.Conn
}

// Read 实现 io.Reader。
// Read implements io.Reader.
func (p *prefixReader) Read(b []byte) (int, error) {
	if len(p.prefix) > 0 {
		n := copy(b, p.prefix)
		p.prefix = p.prefix[n:]
		return n, nil
	}
	return p.r.Read(b)
}

package ws

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// MessageType 是 WebSocket 消息（帧）类型，数值即 RFC 6455 的 opcode。
// MessageType is a WebSocket message (frame) type; its value is the RFC 6455 opcode.
type MessageType int

// 消息类型常量。Text/Binary 为数据消息，其余为控制消息。
// Message type constants. Text/Binary are data messages; the rest are control messages.
const (
	// TextMessage 表示 UTF-8 文本消息。
	// TextMessage denotes a UTF-8 text message.
	TextMessage MessageType = 1
	// BinaryMessage 表示二进制消息。
	// BinaryMessage denotes a binary message.
	BinaryMessage MessageType = 2
	// CloseMessage 表示关闭控制帧。
	// CloseMessage denotes a close control frame.
	CloseMessage MessageType = 8
	// PingMessage 表示 ping 控制帧。
	// PingMessage denotes a ping control frame.
	PingMessage MessageType = 9
	// PongMessage 表示 pong 控制帧。
	// PongMessage denotes a pong control frame.
	PongMessage MessageType = 10
)

// String 返回消息类型的可读名称。
// String returns a readable name of the message type.
func (t MessageType) String() string {
	switch t {
	case TextMessage:
		return "text"
	case BinaryMessage:
		return "binary"
	case CloseMessage:
		return "close"
	case PingMessage:
		return "ping"
	case PongMessage:
		return "pong"
	default:
		return "MessageType(" + strconv.Itoa(int(t)) + ")"
	}
}

// isData 报告 t 是否为数据消息类型。
// isData reports whether t is a data message type.
func (t MessageType) isData() bool { return t == TextMessage || t == BinaryMessage }

// isControl 报告 t 是否为控制消息类型。
// isControl reports whether t is a control message type.
func (t MessageType) isControl() bool {
	return t == CloseMessage || t == PingMessage || t == PongMessage
}

// 关闭码常量（RFC 6455 §7.4.1）。
// Close code constants (RFC 6455 §7.4.1).
const (
	CloseNormalClosure           = 1000
	CloseGoingAway               = 1001
	CloseProtocolError           = 1002
	CloseUnsupportedData         = 1003
	CloseNoStatusReceived        = 1005 // 仅用于本地报告，不得在线上发送 / local reporting only, never sent
	CloseAbnormalClosure         = 1006 // 仅用于本地报告，不得在线上发送 / local reporting only, never sent
	CloseInvalidFramePayloadData = 1007
	ClosePolicyViolation         = 1008
	CloseMessageTooBig           = 1009
	CloseMandatoryExtension      = 1010
	CloseInternalServerErr       = 1011
)

// 协议与默认值常量。
// Protocol and default value constants.
const (
	// SupportedVersion 是唯一支持的 Sec-WebSocket-Version。
	// SupportedVersion is the only supported Sec-WebSocket-Version.
	SupportedVersion = "13"
	// MaxControlPayload 是控制帧负载的最大字节数。
	// MaxControlPayload is the maximum control frame payload size in bytes.
	MaxControlPayload = 125
	// DefaultReadLimit 是默认的单条消息大小上限（32 MiB）。
	// DefaultReadLimit is the default per-message size limit (32 MiB).
	DefaultReadLimit int64 = 32 << 20
	// DefaultReadBufferSize 是默认读缓冲大小。
	// DefaultReadBufferSize is the default read buffer size.
	DefaultReadBufferSize = 4096
	// DefaultWriteBufferSize 是默认写缓冲大小。
	// DefaultWriteBufferSize is the default write buffer size.
	DefaultWriteBufferSize = 4096
	// DefaultControlTimeout 是自动回复 pong/close 及 Close 发送关闭帧的默认超时。
	// DefaultControlTimeout is the default timeout for automatic pong/close replies and
	// the close frame sent by Close.
	DefaultControlTimeout = 5 * time.Second
)

// 哨兵错误，可用 errors.Is 判断。
// Sentinel errors, usable with errors.Is.
var (
	// ErrBadHandshake 表示握手请求不合法（*HandshakeError 解包为它）。
	// ErrBadHandshake reports an invalid handshake request (*HandshakeError unwraps to it).
	ErrBadHandshake = errors.New("ws: bad handshake")
	// ErrOriginNotAllowed 表示 Origin 校验未通过（*HandshakeError 解包为它）。
	// ErrOriginNotAllowed reports a failed Origin check (*HandshakeError unwraps to it).
	ErrOriginNotAllowed = errors.New("ws: origin not allowed")
	// ErrCloseSent 表示关闭帧已发送，不能再写入。
	// ErrCloseSent reports that a close frame was already sent; no more writes are allowed.
	ErrCloseSent = errors.New("ws: close sent")
	// ErrProtocol 表示对端违反协议，连接已以 1002 关闭。
	// ErrProtocol reports a peer protocol violation; the connection was closed with 1002.
	ErrProtocol = errors.New("ws: protocol error")
	// ErrReadLimit 表示消息超过读取上限，连接已以 1009 关闭。
	// ErrReadLimit reports a message over the read limit; the connection was closed with 1009.
	ErrReadLimit = errors.New("ws: read limit exceeded")
	// ErrInvalidUTF8 表示文本消息或关闭原因不是合法 UTF-8，连接已以 1007 关闭。
	// ErrInvalidUTF8 reports invalid UTF-8 in a text message or close reason; the
	// connection was closed with 1007.
	ErrInvalidUTF8 = errors.New("ws: invalid UTF-8")
	// ErrInvalidMessageType 表示写入时使用了不合法的消息类型。
	// ErrInvalidMessageType reports an invalid message type passed to a write method.
	ErrInvalidMessageType = errors.New("ws: invalid message type")
	// ErrControlTooLarge 表示控制帧负载超过 125 字节。
	// ErrControlTooLarge reports a control frame payload larger than 125 bytes.
	ErrControlTooLarge = errors.New("ws: control frame payload too large")
	// ErrStaleReader 表示 NextReader 返回的 reader 已失效（之后又调用了 NextReader）。
	// ErrStaleReader reports a reader from NextReader that is no longer valid because
	// NextReader was called again.
	ErrStaleReader = errors.New("ws: stale message reader")
)

// CloseError 表示连接因关闭帧（或异常断开）而结束。对端发来关闭帧时 Code/Text 为其内容；
// 关闭帧不带状态码时 Code 为 CloseNoStatusReceived；未收到关闭帧就断开时 Code 为
// CloseAbnormalClosure，且错误链包含底层原因（如 io.ErrUnexpectedEOF）。
// CloseError reports that the connection ended with a close frame (or abnormally). When
// the peer sent a close frame, Code/Text carry its content; a close frame without status
// yields CloseNoStatusReceived; a disconnect without close frame yields
// CloseAbnormalClosure and the error chain contains the cause (e.g. io.ErrUnexpectedEOF).
type CloseError struct {
	// Code 是关闭码。
	// Code is the close code.
	Code int
	// Text 是关闭原因。
	// Text is the close reason.
	Text string

	// err 是异常关闭的底层原因，可为 nil。
	// err is the underlying cause of an abnormal closure; may be nil.
	err error
}

// Error 实现 error。
// Error implements error.
func (e *CloseError) Error() string {
	s := "ws: close " + strconv.Itoa(e.Code)
	if e.Text != "" {
		s += " " + e.Text
	}
	if e.err != nil {
		s += ": " + e.err.Error()
	}
	return s
}

// Unwrap 返回异常关闭的底层原因。
// Unwrap returns the underlying cause of an abnormal closure.
func (e *CloseError) Unwrap() error { return e.err }

// IsCloseError 报告 err 是否为 *CloseError 且关闭码属于 codes；codes 为空时只判断类型。
// IsCloseError reports whether err is a *CloseError whose code is in codes; with no
// codes it only checks the type.
func IsCloseError(err error, codes ...int) bool {
	var ce *CloseError
	if !errors.As(err, &ce) {
		return false
	}
	if len(codes) == 0 {
		return true
	}
	for _, c := range codes {
		if ce.Code == c {
			return true
		}
	}
	return false
}

// HandshakeError 描述握手失败：Status 是已写出的 HTTP 状态码，Reason 是原因。
// 它解包为 ErrBadHandshake 或 ErrOriginNotAllowed。
// HandshakeError describes a failed handshake: Status is the HTTP status written and
// Reason the cause. It unwraps to ErrBadHandshake or ErrOriginNotAllowed.
type HandshakeError struct {
	// Status 是写给客户端的 HTTP 状态码。
	// Status is the HTTP status code written to the client.
	Status int
	// Reason 是失败原因。
	// Reason is the failure reason.
	Reason string

	// kind 是哨兵错误。
	// kind is the sentinel error.
	kind error
}

// Error 实现 error。
// Error implements error.
func (e *HandshakeError) Error() string {
	return fmt.Sprintf("%v: %s (status %d)", e.kind, e.Reason, e.Status)
}

// Unwrap 返回哨兵错误。
// Unwrap returns the sentinel error.
func (e *HandshakeError) Unwrap() error { return e.kind }

// failError 是本端因对端违规而关闭连接时返回的错误，解包为对应哨兵错误。
// failError is returned when this side fails the connection because of a peer
// violation; it unwraps to the matching sentinel error.
type failError struct {
	code   int
	reason string
	kind   error
}

// Error 实现 error。
// Error implements error.
func (e *failError) Error() string {
	return fmt.Sprintf("%v: %s (closed with %d)", e.kind, e.reason, e.code)
}

// Unwrap 返回哨兵错误。
// Unwrap returns the sentinel error.
func (e *failError) Unwrap() error { return e.kind }

// FormatCloseMessage 构造关闭帧负载。code 为 CloseNoStatusReceived 或 <= 0 时返回空负载；
// text 超出 123 字节时按 UTF-8 边界截断。
// FormatCloseMessage builds a close frame payload. It returns an empty payload when code
// is CloseNoStatusReceived or <= 0; text over 123 bytes is truncated at a UTF-8 boundary.
func FormatCloseMessage(code int, text string) []byte {
	if code <= 0 || code == CloseNoStatusReceived {
		return []byte{}
	}
	text = truncateUTF8(text, MaxControlPayload-2)
	buf := make([]byte, 2+len(text))
	binary.BigEndian.PutUint16(buf, uint16(code))
	copy(buf[2:], text)
	return buf
}

// truncateUTF8 把 s 截断到最多 n 字节，且不切断多字节字符。
// truncateUTF8 truncates s to at most n bytes without splitting a multi-byte character.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// Package mux 提供链路复用：在单条 net.Conn 上承载多条逻辑流。
// 默认帧协议与 hashicorp/yamux 兼容（12 字节大端帧头、Data/WindowUpdate/
// Ping/GoAway 四类帧、SYN/ACK/FIN/RST 位域、逐流窗口流控），
// 任何满足 net.Conn 的连接（reactor Conn、TLS 等）均可作为底层承载。
//
// Package mux provides connection multiplexing: many logical streams over one
// net.Conn. The default framing is compatible with hashicorp/yamux (12-byte
// big-endian header; Data/WindowUpdate/Ping/GoAway frame types; SYN/ACK/FIN/RST
// flags; per-stream window flow control), and any net.Conn (reactor Conn, TLS,
// ...) can serve as the transport.
package mux

import (
	"encoding/binary"
	"errors"
	"time"
)

// 帧头常量（与 yamux 帧规格一致）。
// Frame constants (matching the yamux frame spec).
const (
	headerSize = 12

	typeData         uint8 = 0x0
	typeWindowUpdate uint8 = 0x1
	typePing         uint8 = 0x2
	typeGoAway       uint8 = 0x3

	flagSYN uint16 = 0x1
	flagACK uint16 = 0x2
	flagFIN uint16 = 0x4
	flagRST uint16 = 0x8
)

// 默认参数。
// Default parameters.
const (
	defaultAcceptBacklog = 256
	defaultStreamWindow  = 256 * 1024
	defaultWriteTimeout  = 10 * time.Second
)

// 错误。
// Errors.
var (
	// ErrStreamClosed 表示流已关闭（对端 FIN/RST 或本端关闭）。
	ErrStreamClosed = errors.New("mux: stream closed")
	// ErrSessionShutdown 表示会话已关闭。
	ErrSessionShutdown = errors.New("mux: session shutdown")
	// ErrKeepAliveTimeout 表示 keepalive 超时。
	ErrKeepAliveTimeout = errors.New("mux: keepalive timeout")
	// ErrRecvWindowExceeded 表示对端发送量超过接收窗口。
	ErrRecvWindowExceeded = errors.New("mux: receive window exceeded")
	// ErrGoAway 表示会话已进入 go-away 状态、拒绝新流。
	ErrGoAway = errors.New("mux: session is going away")
	// ErrStreamsExhausted 表示流 ID 耗尽。
	ErrStreamsExhausted = errors.New("mux: streams exhausted")
	// ErrTimeout 是满足 net.Error 的超时错误。
	ErrTimeout = &netError{err: errors.New("mux: i/o deadline reached"), timeout: true}
)

// netError 实现 net.Error。
// netError implements net.Error.
type netError struct {
	err     error
	timeout bool
}

func (e *netError) Error() string   { return e.err.Error() }
func (e *netError) Timeout() bool   { return e.timeout }
func (e *netError) Temporary() bool { return false }
func (e *netError) Unwrap() error   { return e.err }

// header 是 12 字节大端帧头。
// header is the 12-byte big-endian frame header.
type header struct {
	Version  uint8
	Type     uint8
	Flags    uint16
	StreamID uint32
	Length   uint32
}

// encode 写入帧头。
// encode writes the header.
func (h header) encode(buf []byte) {
	buf[0] = h.Version
	buf[1] = h.Type
	binary.BigEndian.PutUint16(buf[2:4], h.Flags)
	binary.BigEndian.PutUint32(buf[4:8], h.StreamID)
	binary.BigEndian.PutUint32(buf[8:12], h.Length)
}

// decodeHeader 解析帧头。
// decodeHeader parses a frame header.
func decodeHeader(buf []byte) header {
	return header{
		Version:  buf[0],
		Type:     buf[1],
		Flags:    binary.BigEndian.Uint16(buf[2:4]),
		StreamID: binary.BigEndian.Uint32(buf[4:8]),
		Length:   binary.BigEndian.Uint32(buf[8:12]),
	}
}

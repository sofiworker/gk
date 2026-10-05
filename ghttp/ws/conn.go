package ws

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// maxInitialReadCap 是 ReadMessage 初始分配的上限，避免对端声明超大帧却不发送数据造成的内存放大。
// maxInitialReadCap caps the initial allocation of ReadMessage, so a peer announcing a huge
// frame without sending it cannot amplify memory use.
const maxInitialReadCap = 64 << 10

// Conn 是一条已升级的 WebSocket 连接。
//
// 同一时刻最多一个 goroutine 调用读方法（ReadMessage/NextReader）；写方法
// （WriteMessage/WriteControl/Ping/Close）内部互斥，可被多个 goroutine 并发调用，
// 每条消息作为单个帧原子写出，控制帧不会插入数据帧中间。
//
// Conn is an upgraded WebSocket connection.
//
// At most one goroutine may call the read methods (ReadMessage/NextReader) at a time;
// the write methods (WriteMessage/WriteControl/Ping/Close) are mutually exclusive
// internally and may be called from multiple goroutines. Each message is written
// atomically as a single frame, so control frames never interleave with data frames.
type Conn struct {
	conn        net.Conn
	server      bool
	subprotocol string
	cfg         *config

	// 写侧，由 wlock 保护（除 writeDeadline 外）。
	// Write side, guarded by wlock (except writeDeadline).
	wlock         chan struct{}
	bw            *bufio.Writer
	hdr           [maxFrameHeaderSize]byte
	closeSent     bool
	writeErr      error
	writeDeadline atomic.Int64

	// 读侧，仅由读 goroutine 访问。
	// Read side, accessed only by the reading goroutine.
	br        *bufio.Reader
	readErr   error
	ctrl      [MaxControlPayload]byte
	cur       *messageReader
	frame     frameHeader
	remaining int64
	maskPos   int
	msgType   MessageType
	msgLen    int64
	utf8      utf8Validator

	closeOnce sync.Once
	closed    atomic.Bool
}

// newConn 用已完成握手的连接构造 Conn。server 决定掩码方向：服务端要求读到的帧带掩码、
// 写出的帧不带掩码；客户端相反。
// newConn builds a Conn on a connection that finished the handshake. server decides the
// masking direction: a server requires masked incoming frames and writes unmasked ones;
// a client does the opposite.
func newConn(conn net.Conn, br *bufio.Reader, bw *bufio.Writer, server bool, subprotocol string, cfg *config) *Conn {
	return &Conn{
		conn:        conn,
		server:      server,
		subprotocol: subprotocol,
		cfg:         cfg,
		wlock:       make(chan struct{}, 1),
		bw:          bw,
		br:          br,
	}
}

// Subprotocol 返回协商出的子协议；未协商时为空串。
// Subprotocol returns the negotiated subprotocol, or "" when none was negotiated.
func (c *Conn) Subprotocol() string { return c.subprotocol }

// RemoteAddr 返回对端地址。
// RemoteAddr returns the remote network address.
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// LocalAddr 返回本端地址。
// LocalAddr returns the local network address.
func (c *Conn) LocalAddr() net.Addr { return c.conn.LocalAddr() }

// NetConn 返回底层连接（如需设置 TCP 选项）。直接读写会破坏帧流。
// NetConn returns the underlying connection (e.g. for TCP options). Reading or writing
// it directly corrupts the frame stream.
func (c *Conn) NetConn() net.Conn { return c.conn }

// SetReadDeadline 设置读超时；超时后连接不可再读。零值表示不超时。
// SetReadDeadline sets the read deadline; after a timeout the connection can no longer be
// read. A zero value means no deadline.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// SetWriteDeadline 设置之后数据写入使用的超时，并立即作用于进行中的写。零值表示不超时。
// 写超时后连接不可再写。
// SetWriteDeadline sets the deadline used by subsequent data writes and applies it to
// any write in progress. A zero value means no deadline. After a write timeout the
// connection can no longer be written.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	if t.IsZero() {
		c.writeDeadline.Store(0)
	} else {
		c.writeDeadline.Store(t.UnixNano())
	}
	return c.conn.SetWriteDeadline(t)
}

// loadWriteDeadline 返回 SetWriteDeadline 设置的值。
// loadWriteDeadline returns the value set by SetWriteDeadline.
func (c *Conn) loadWriteDeadline() time.Time {
	v := c.writeDeadline.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// ---------------------------------------------------------------------------
// 写 / Write
// ---------------------------------------------------------------------------

// WriteMessage 把 data 作为一条完整消息（单帧）写出。mt 必须是 TextMessage 或
// BinaryMessage；文本内容的 UTF-8 合法性由调用方保证。
// WriteMessage writes data as one complete message (a single frame). mt must be
// TextMessage or BinaryMessage; the caller is responsible for valid UTF-8 text.
func (c *Conn) WriteMessage(mt MessageType, data []byte) error {
	if !mt.isData() {
		return fmt.Errorf("%w: %v", ErrInvalidMessageType, mt)
	}
	return c.writeFrame(byte(mt), data, time.Time{})
}

// WriteControl 写出控制帧（CloseMessage/PingMessage/PongMessage），负载不超过 125 字节。
// deadline 同时限制等待写锁与写出的时间；零值表示使用 SetWriteDeadline 的设置。
// 写出 CloseMessage 后不能再写任何帧。
// WriteControl writes a control frame (CloseMessage/PingMessage/PongMessage) with a
// payload of at most 125 bytes. deadline bounds both waiting for the write lock and the
// write itself; a zero value uses the SetWriteDeadline setting. No frame can be written
// after a CloseMessage.
func (c *Conn) WriteControl(mt MessageType, data []byte, deadline time.Time) error {
	if !mt.isControl() {
		return fmt.Errorf("%w: %v", ErrInvalidMessageType, mt)
	}
	if len(data) > MaxControlPayload {
		return ErrControlTooLarge
	}
	return c.writeFrame(byte(mt), data, deadline)
}

// Ping 发送 ping 控制帧，超时为 WithControlTimeout 的设置。
// Ping sends a ping control frame with the WithControlTimeout timeout.
func (c *Conn) Ping(data []byte) error {
	return c.WriteControl(PingMessage, data, time.Now().Add(c.cfg.controlTimeout))
}

// Close 发送关闭帧（code <= 0 表示不带状态码；reason 超过 123 字节会被截断）后关闭底层连接。
// 服务端按 RFC 6455 先关闭 TCP，不等待对端回复关闭帧。可重复调用，之后的调用返回 nil。
// Close sends a close frame (code <= 0 means no status; reason over 123 bytes is
// truncated) and closes the underlying connection. As a server it closes TCP first per
// RFC 6455 without waiting for the peer's close frame. It is idempotent; later calls
// return nil.
func (c *Conn) Close(code int, reason string) error {
	if c.closed.Load() {
		return nil
	}
	err := c.writeFrame(opClose, FormatCloseMessage(code, reason), time.Now().Add(c.cfg.controlTimeout))
	if errors.Is(err, ErrCloseSent) || c.closed.Load() {
		err = nil
	}
	if cerr := c.closeConn(); err == nil {
		err = cerr
	}
	return err
}

// closeConn 关闭底层连接一次。
// closeConn closes the underlying connection once.
func (c *Conn) closeConn() error {
	var err error
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		err = c.conn.Close()
	})
	return err
}

// lockWrite 获取写锁；deadline 非零时最多等到 deadline。
// lockWrite acquires the write lock, waiting at most until deadline when it is non-zero.
func (c *Conn) lockWrite(deadline time.Time) error {
	if deadline.IsZero() {
		c.wlock <- struct{}{}
		return nil
	}
	select {
	case c.wlock <- struct{}{}:
		return nil
	default:
	}
	d := time.Until(deadline)
	if d <= 0 {
		return fmt.Errorf("ws: waiting for write lock: %w", os.ErrDeadlineExceeded)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case c.wlock <- struct{}{}:
		return nil
	case <-t.C:
		return fmt.Errorf("ws: waiting for write lock: %w", os.ErrDeadlineExceeded)
	}
}

// unlockWrite 释放写锁。
// unlockWrite releases the write lock.
func (c *Conn) unlockWrite() { <-c.wlock }

// writeFrame 在写锁内写出一个 FIN 帧。写失败后错误是粘滞的。
// writeFrame writes one FIN frame under the write lock. Write errors are sticky.
func (c *Conn) writeFrame(opcode byte, data []byte, deadline time.Time) error {
	if err := c.lockWrite(deadline); err != nil {
		return err
	}
	defer c.unlockWrite()

	if c.closeSent {
		return ErrCloseSent
	}
	if c.writeErr != nil {
		return c.writeErr
	}
	d := deadline
	if d.IsZero() {
		d = c.loadWriteDeadline()
	}
	if err := c.conn.SetWriteDeadline(d); err != nil {
		return err
	}

	var key [4]byte
	masked := !c.server
	if masked {
		_, _ = rand.Read(key[:]) // crypto/rand.Read 不会失败 / never fails
	}
	_, _ = c.bw.Write(appendFrameHeader(c.hdr[:0], true, opcode, len(data), masked, key))
	if masked {
		c.writeMasked(data, key)
	} else {
		_, _ = c.bw.Write(data)
	}
	if opcode == opClose {
		c.closeSent = true
	}
	// bufio.Writer 的错误是粘滞的，Flush 会返回此前任何写错误。
	// bufio.Writer errors are sticky; Flush reports any earlier write error.
	if err := c.bw.Flush(); err != nil {
		c.writeErr = err
		return err
	}
	return nil
}

// writeMasked 把 data 掩码后写入缓冲，不修改调用方的切片。
// writeMasked writes data masked into the buffer without modifying the caller's slice.
func (c *Conn) writeMasked(data []byte, key [4]byte) {
	pos := 0
	for len(data) > 0 {
		buf := c.bw.AvailableBuffer()
		if cap(buf) == 0 {
			if err := c.bw.Flush(); err != nil {
				return
			}
			buf = c.bw.AvailableBuffer()
		}
		n := min(cap(buf), len(data))
		buf = append(buf, data[:n]...)
		pos = maskBytes(key, pos, buf)
		_, _ = c.bw.Write(buf)
		data = data[n:]
	}
}

// ---------------------------------------------------------------------------
// 读 / Read
// ---------------------------------------------------------------------------

// ReadMessage 读取下一条完整数据消息（自动重组分片、处理控制帧）。
// 对端关闭时返回 *CloseError；对端违反协议时连接已被关闭，错误解包为
// ErrProtocol/ErrReadLimit/ErrInvalidUTF8。读错误是粘滞的。
// ReadMessage reads the next complete data message (reassembling fragments and handling
// control frames). It returns a *CloseError when the peer closes; on a peer protocol
// violation the connection has been closed and the error unwraps to
// ErrProtocol/ErrReadLimit/ErrInvalidUTF8. Read errors are sticky.
func (c *Conn) ReadMessage() (MessageType, []byte, error) {
	mt, r, err := c.NextReader()
	if err != nil {
		return 0, nil, err
	}
	buf := make([]byte, 0, min(c.remaining, maxInitialReadCap))
	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err == io.EOF {
			return mt, buf, nil
		}
		if err != nil {
			return 0, nil, err
		}
	}
}

// NextReader 返回下一条数据消息的类型与流式 reader。reader 在消息结束时返回 io.EOF；
// 再次调用 NextReader 会丢弃上一条消息的剩余数据，旧 reader 随即返回 ErrStaleReader。
// NextReader returns the type and a streaming reader of the next data message. The
// reader returns io.EOF at the end of the message; calling NextReader again discards the
// rest of the previous message and the old reader then returns ErrStaleReader.
func (c *Conn) NextReader() (MessageType, io.Reader, error) {
	if old := c.cur; old != nil {
		if !old.eof && c.readErr == nil {
			_, _ = io.Copy(io.Discard, old)
		}
		old.stale = true
		c.cur = nil
	}
	if c.readErr != nil {
		return 0, nil, c.readErr
	}

	c.msgLen = 0
	c.utf8.done() // 重置 / reset
	h, err := c.nextDataFrame()
	if err != nil {
		return 0, nil, err
	}
	if h.opcode == opContinuation {
		return 0, nil, c.fail(CloseProtocolError, ErrProtocol, "continuation frame without a started message")
	}
	c.msgType = MessageType(h.opcode)
	c.cur = &messageReader{c: c}
	return c.msgType, c.cur, nil
}

// nextDataFrame 读取帧头直到遇到数据帧，途中处理控制帧并执行协议校验。
// nextDataFrame reads frame headers until a data frame, handling control frames and
// enforcing protocol checks on the way.
func (c *Conn) nextDataFrame() (frameHeader, error) {
	for {
		h, err := readFrameHeader(c.br)
		if err != nil {
			return h, c.readFailure(err)
		}
		switch {
		case h.rsv != 0:
			return h, c.fail(CloseProtocolError, ErrProtocol, "reserved bits set without negotiated extension")
		case !isKnownOpcode(h.opcode):
			return h, c.fail(CloseProtocolError, ErrProtocol, "reserved opcode")
		case c.server && !h.masked:
			return h, c.fail(CloseProtocolError, ErrProtocol, "client frame is not masked")
		case !c.server && h.masked:
			return h, c.fail(CloseProtocolError, ErrProtocol, "server frame is masked")
		}
		if isControlOpcode(h.opcode) {
			if !h.fin {
				return h, c.fail(CloseProtocolError, ErrProtocol, "fragmented control frame")
			}
			if h.length > MaxControlPayload {
				return h, c.fail(CloseProtocolError, ErrProtocol, "control frame payload too large")
			}
			if err := c.handleControl(h); err != nil {
				return h, err
			}
			continue
		}
		if h.length > c.cfg.readLimit-c.msgLen {
			return h, c.fail(CloseMessageTooBig, ErrReadLimit, "message exceeds read limit")
		}
		c.msgLen += h.length
		c.frame = h
		c.remaining = h.length
		c.maskPos = 0
		return h, nil
	}
}

// handleControl 读取控制帧负载并分派处理。
// handleControl reads a control frame payload and dispatches it.
func (c *Conn) handleControl(h frameHeader) error {
	p := c.ctrl[:h.length]
	if _, err := io.ReadFull(c.br, p); err != nil {
		return c.readFailure(err)
	}
	if h.masked {
		maskBytes(h.key, 0, p)
	}
	var err error
	switch h.opcode {
	case opPing:
		if c.cfg.pingHandler != nil {
			err = c.cfg.pingHandler(c, p)
		} else {
			err = c.replyPong(p)
		}
	case opPong:
		if c.cfg.pongHandler != nil {
			err = c.cfg.pongHandler(c, p)
		}
	case opClose:
		return c.handleClose(p)
	}
	if err != nil {
		c.readErr = err
	}
	return err
}

// replyPong 是默认 ping 处理：回复同负载的 pong。关闭已发送或写超时不视为读错误。
// replyPong is the default ping handling: reply with a pong carrying the same payload.
// A sent close or a write timeout is not treated as a read error.
func (c *Conn) replyPong(p []byte) error {
	err := c.WriteControl(PongMessage, p, time.Now().Add(c.cfg.controlTimeout))
	if err == nil || errors.Is(err, ErrCloseSent) || errors.Is(err, os.ErrDeadlineExceeded) {
		return nil
	}
	return err
}

// handleClose 处理对端关闭帧：回复关闭帧（服务端随后关闭 TCP），返回 *CloseError。
// handleClose handles the peer's close frame: it replies with a close frame (a server
// then closes TCP) and returns a *CloseError.
func (c *Conn) handleClose(p []byte) error {
	code, text, ferr := parseClosePayload(p)
	if ferr != nil {
		return c.fail(ferr.code, ferr.kind, ferr.reason)
	}
	_ = c.writeFrame(opClose, FormatCloseMessage(code, ""), time.Now().Add(c.cfg.controlTimeout))
	if c.server {
		_ = c.closeConn()
	}
	c.readErr = &CloseError{Code: code, Text: text}
	return c.readErr
}

// fail 以 code 关闭连接（尽力发送关闭帧），记录并返回粘滞的读错误。
// fail closes the connection with code (sending a close frame best-effort), records and
// returns the sticky read error.
func (c *Conn) fail(code int, kind error, reason string) error {
	err := &failError{code: code, reason: reason, kind: kind}
	c.readErr = err
	_ = c.writeFrame(opClose, FormatCloseMessage(code, reason), time.Now().Add(c.cfg.controlTimeout))
	_ = c.closeConn()
	return err
}

// readFailure 把底层读错误转换为粘滞的读错误：EOF 视为异常关闭（1006）。
// readFailure turns an underlying read error into the sticky read error: EOF is treated
// as an abnormal closure (1006).
func (c *Conn) readFailure(err error) error {
	var fe *failError
	switch {
	case errors.As(err, &fe):
		return c.fail(fe.code, fe.kind, fe.reason)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		c.readErr = &CloseError{Code: CloseAbnormalClosure, err: io.ErrUnexpectedEOF}
	default:
		c.readErr = err
	}
	return c.readErr
}

// messageReader 流式读取当前数据消息的负载。
// messageReader streams the payload of the current data message.
type messageReader struct {
	c     *Conn
	eof   bool
	stale bool
}

// Read 实现 io.Reader：跨分片读取并解掩码，途中处理控制帧，文本消息增量校验 UTF-8。
// Read implements io.Reader: it reads and unmasks across fragments, handles control
// frames on the way and validates text messages incrementally as UTF-8.
func (r *messageReader) Read(p []byte) (int, error) {
	c := r.c
	switch {
	case r.stale:
		return 0, ErrStaleReader
	case r.eof:
		return 0, io.EOF
	case c.readErr != nil:
		return 0, c.readErr
	}
	for {
		if c.remaining > 0 {
			if len(p) == 0 {
				return 0, nil
			}
			if int64(len(p)) > c.remaining {
				p = p[:c.remaining]
			}
			n, err := c.br.Read(p)
			if n > 0 {
				data := p[:n]
				if c.frame.masked {
					c.maskPos = maskBytes(c.frame.key, c.maskPos, data)
				}
				c.remaining -= int64(n)
				if c.msgType == TextMessage && !c.utf8.write(data) {
					return 0, c.fail(CloseInvalidFramePayloadData, ErrInvalidUTF8, "invalid UTF-8 in text message")
				}
				return n, nil
			}
			if err != nil {
				return 0, c.readFailure(err)
			}
			continue
		}
		if c.frame.fin {
			if c.msgType == TextMessage && !c.utf8.done() {
				return 0, c.fail(CloseInvalidFramePayloadData, ErrInvalidUTF8, "invalid UTF-8 in text message")
			}
			r.eof = true
			return 0, io.EOF
		}
		h, err := c.nextDataFrame()
		if err != nil {
			return 0, err
		}
		if h.opcode != opContinuation {
			return 0, c.fail(CloseProtocolError, ErrProtocol, "new data frame before the previous message finished")
		}
	}
}

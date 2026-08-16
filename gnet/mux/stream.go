package mux

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// Stream 是复用流：满足 io.ReadWriteCloser，支持半关闭与截止时间。
// Stream is a multiplexed stream: io.ReadWriteCloser with half-close and
// deadlines.
type Stream interface {
	io.Reader
	io.Writer
	StreamID() uint32
	Close() error
	CloseWrite() error
	CloseRead() error
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// stream 是 Stream 的实现。
// stream implements Stream.
type stream struct {
	session *Session
	id      uint32

	// 读侧
	readMu     sync.Mutex
	recvBuf    []byte
	recvWindow uint32
	readWait   chan struct{}
	readClosed bool
	finRecv    bool
	readDL     time.Time

	// 写侧
	writeMu     sync.Mutex
	sendWindow  uint32
	windowInit  bool // 是否已收到对端的窗口通告
	writeWait   chan struct{}
	writeClosed bool
	writeDL     time.Time

	done chan struct{}
	once sync.Once
}

// StreamID 返回流 ID。
// StreamID returns the stream id.
func (s *stream) StreamID() uint32 { return s.id }

// recvWindowValue 返回当前接收窗口（SYN/ACK 通告用）。
// recvWindowValue returns the current receive window for SYN/ACK ads.
func (s *stream) recvWindowValue() uint32 {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	return s.recvWindow
}

// setSendWindow 设置对端通告的初始发送窗口。
// setSendWindow sets the peer-advertised initial send window.
func (s *stream) setSendWindow(n uint32) {
	s.writeMu.Lock()
	s.sendWindow = n
	s.windowInit = true
	s.writeMu.Unlock()
}

// recvWindowUpdate 处理窗口通告：首个通告（含 SYN）为初始窗口（设置），
// 其后为增量（累加）。
//
// recvWindowUpdate applies a window advertisement: the first one (including
// SYN) sets the initial window; later ones add a delta.
func (s *stream) recvWindowUpdate(h header) {
	s.writeMu.Lock()
	if !s.windowInit || h.Flags&flagSYN != 0 {
		s.sendWindow = h.Length
		s.windowInit = true
	} else {
		s.sendWindow += h.Length
	}
	wake := len(s.writeWait) == 0
	s.writeMu.Unlock()
	if wake {
		select {
		case s.writeWait <- struct{}{}:
		default:
		}
	}
}

// recvData 投递数据到读缓冲；超窗则 RST 并报错。
// recvData appends data to the read buffer; RST + error on window overflow.
func (s *stream) recvData(body []byte) error {
	s.readMu.Lock()
	if s.readClosed {
		s.readMu.Unlock()
		return nil
	}
	if uint32(len(body)) > s.recvWindow {
		s.readMu.Unlock()
		_ = s.session.sendFrame(header{Type: typeData, Flags: flagRST, StreamID: s.id}, nil)
		s.recvRST()
		return ErrRecvWindowExceeded
	}
	s.recvBuf = append(s.recvBuf, body...)
	// recvWindow 语义：对端可发送的剩余预算。发出 WindowUpdate 时把
	// 增量加回预算（yamux 语义），否则对端用完预算后会触发误报 RST。
	// recvWindow semantics: the peer's remaining send budget. Restore the
	// granted delta when sending a WindowUpdate (yamux semantics), otherwise
	// the peer trips a spurious RST once the budget is spent.
	s.recvWindow -= uint32(len(body))
	needUpdate := s.recvWindow < s.session.cfg.MaxStreamWindowSize/2
	delta := s.session.cfg.MaxStreamWindowSize - s.recvWindow
	if needUpdate {
		s.recvWindow += delta
	}
	s.readMu.Unlock()

	select {
	case s.readWait <- struct{}{}:
	default:
	}
	if needUpdate {
		_ = s.session.sendFrame(header{Type: typeWindowUpdate, StreamID: s.id, Length: delta}, nil)
	}
	return nil
}

// recvFIN 处理对端半关闭写侧。
// recvFIN handles the peer half-closing its write side.
func (s *stream) recvFIN() {
	s.readMu.Lock()
	s.finRecv = true
	s.readMu.Unlock()
	select {
	case s.readWait <- struct{}{}:
	default:
	}
	s.maybeRemove()
}

// recvRST 处理对端复位：双端立即关闭。
// recvRST handles a peer reset: both sides close immediately.
func (s *stream) recvRST() {
	s.readMu.Lock()
	s.readClosed = true
	s.recvBuf = nil
	s.readMu.Unlock()
	select {
	case s.readWait <- struct{}{}:
	default:
	}
	s.writeMu.Lock()
	s.writeClosed = true
	s.writeMu.Unlock()
	select {
	case s.writeWait <- struct{}{}:
	default:
	}
	s.markDead()
	s.session.removeStream(s, nil)
}

// maybeRemove 双端 FIN 后从会话移除。
// maybeRemove removes the stream once both sides are FIN.
func (s *stream) maybeRemove() {
	s.readMu.Lock()
	recvDone := s.finRecv
	s.readMu.Unlock()
	s.writeMu.Lock()
	writeDone := s.writeClosed
	s.writeMu.Unlock()
	if recvDone && writeDone {
		s.markDead()
		s.session.removeStream(s, nil)
	}
}

// sessionClosed 会话关闭时终结流。
// sessionClosed finalizes the stream on session shutdown.
func (s *stream) sessionClosed() {
	s.readMu.Lock()
	s.readClosed = true
	s.recvBuf = nil
	s.readMu.Unlock()
	select {
	case s.readWait <- struct{}{}:
	default:
	}
	s.writeMu.Lock()
	s.writeClosed = true
	s.writeMu.Unlock()
	select {
	case s.writeWait <- struct{}{}:
	default:
	}
	s.markDead()
}

// markDead 关闭 done 通道。
// markDead closes the done channel.
func (s *stream) markDead() {
	s.once.Do(func() { close(s.done) })
}

// Read 读取数据；对端 FIN 且缓冲排空后返回 io.EOF。
// Read reads data; returns io.EOF after the peer FIN drains the buffer.
func (s *stream) Read(p []byte) (int, error) {
	for {
		s.readMu.Lock()
		if len(s.recvBuf) > 0 {
			n := copy(p, s.recvBuf)
			s.recvBuf = s.recvBuf[n:]
			s.readMu.Unlock()
			return n, nil
		}
		if s.finRecv {
			s.readMu.Unlock()
			return 0, io.EOF
		}
		if s.readClosed {
			s.readMu.Unlock()
			return 0, ErrStreamClosed
		}
		dl := s.readDL
		wait := s.readWait
		done := s.done
		sessionClosed := s.session.closed
		s.readMu.Unlock()

		if !dl.IsZero() && time.Now().After(dl) {
			return 0, os.ErrDeadlineExceeded
		}
		if dl.IsZero() {
			select {
			case <-wait:
			case <-done:
				return 0, ErrStreamClosed
			case <-sessionClosed:
				return 0, s.session.shutdownErr()
			}
			continue
		}
		timer := time.NewTimer(time.Until(dl))
		select {
		case <-wait:
			timer.Stop()
		case <-timer.C:
			s.readMu.Lock()
			if len(s.recvBuf) > 0 {
				n := copy(p, s.recvBuf)
				s.recvBuf = s.recvBuf[n:]
				s.readMu.Unlock()
				return n, nil
			}
			s.readMu.Unlock()
			return 0, os.ErrDeadlineExceeded
		case <-done:
			timer.Stop()
			return 0, ErrStreamClosed
		case <-sessionClosed:
			timer.Stop()
			return 0, s.session.shutdownErr()
		}
	}
}

// Write 写入数据：受发送窗口流控，写满返回 len(p)。
// Write writes data: send-window flow controlled, returns len(p) on success.
func (s *stream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	total := 0
	for len(p) > 0 {
		s.writeMu.Lock()
		if s.writeClosed {
			s.writeMu.Unlock()
			return total, ErrStreamClosed
		}
		if !s.writeDL.IsZero() && time.Now().After(s.writeDL) {
			s.writeMu.Unlock()
			return total, os.ErrDeadlineExceeded
		}
		if s.sendWindow == 0 {
			wait := s.writeWait
			done := s.done
			sessionClosed := s.session.closed
			s.writeMu.Unlock()

			var err error
			if s.writeDL.IsZero() {
				select {
				case <-wait:
				case <-done:
					err = ErrStreamClosed
				case <-sessionClosed:
					err = s.session.shutdownErr()
				}
			} else {
				timer := time.NewTimer(time.Until(s.writeDL))
				select {
				case <-wait:
					timer.Stop()
				case <-timer.C:
					err = os.ErrDeadlineExceeded
				case <-done:
					timer.Stop()
					err = ErrStreamClosed
				case <-sessionClosed:
					timer.Stop()
					err = s.session.shutdownErr()
				}
			}
			if err != nil {
				return total, err
			}
			continue
		}
		n := len(p)
		if uint32(n) > s.sendWindow {
			n = int(s.sendWindow)
		}
		s.sendWindow -= uint32(n)
		chunk := p[:n]
		s.writeMu.Unlock()

		if err := s.session.sendFrame(header{Type: typeData, StreamID: s.id, Length: uint32(len(chunk))}, chunk); err != nil {
			return total, err
		}
		p = p[n:]
		total += n
	}
	return total, nil
}

// CloseWrite 半关闭写侧：发送 FIN（此前的 Write 数据先于 FIN 到达）。
// CloseWrite half-closes the write side: sends FIN (previous writes precede
// it).
func (s *stream) CloseWrite() error {
	s.writeMu.Lock()
	if s.writeClosed {
		s.writeMu.Unlock()
		return nil
	}
	s.writeClosed = true
	s.writeMu.Unlock()
	err := s.session.sendFrame(header{Type: typeData, Flags: flagFIN, StreamID: s.id}, nil)
	s.maybeRemove()
	return err
}

// CloseRead 半关闭读侧：丢弃缓冲，后续数据静默丢弃。
// CloseRead half-closes the read side: discards the buffer; later data is
// silently dropped.
func (s *stream) CloseRead() error {
	s.readMu.Lock()
	s.readClosed = true
	s.recvBuf = nil
	s.readMu.Unlock()
	select {
	case s.readWait <- struct{}{}:
	default:
	}
	s.maybeRemove()
	return nil
}

// Close 全关闭：FIN + 丢弃未读。
// Close fully closes: FIN plus unread discard.
func (s *stream) Close() error {
	err := s.CloseWrite()
	_ = s.CloseRead()
	return err
}

// SetDeadline 同时设置读写截止。
// SetDeadline sets both read and write deadlines.
func (s *stream) SetDeadline(t time.Time) error {
	s.readMu.Lock()
	s.readDL = t
	s.readMu.Unlock()
	s.writeMu.Lock()
	s.writeDL = t
	s.writeMu.Unlock()
	return nil
}

// SetReadDeadline 设置读截止。
// SetReadDeadline sets the read deadline.
func (s *stream) SetReadDeadline(t time.Time) error {
	s.readMu.Lock()
	s.readDL = t
	s.readMu.Unlock()
	return nil
}

// SetWriteDeadline 设置写截止。
// SetWriteDeadline sets the write deadline.
func (s *stream) SetWriteDeadline(t time.Time) error {
	s.writeMu.Lock()
	s.writeDL = t
	s.writeMu.Unlock()
	return nil
}

// isTimeout 判断错误是否超时（测试辅助）。
// isTimeout reports whether err is a timeout error (test helper).
func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

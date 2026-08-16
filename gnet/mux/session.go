package mux

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// Config 是会话配置。
// Config holds session configuration.
type Config struct {
	// AcceptBacklog 待接受流的缓冲上限（默认 256）。
	AcceptBacklog int
	// MaxStreamWindowSize 每流窗口大小（默认 256KB）。
	MaxStreamWindowSize uint32
	// KeepAliveInterval keepalive ping 间隔（默认 30s；<=0 关闭）。
	KeepAliveInterval time.Duration
	// ConnectionWriteTimeout 帧写出与 keepalive 响应超时（默认 10s）。
	ConnectionWriteTimeout time.Duration
}

// Option 配置会话。
// Option configures a session.
type Option func(*Config)

// WithAcceptBacklog 设置待接受流缓冲上限。
// WithAcceptBacklog sets the accept backlog.
func WithAcceptBacklog(n int) Option {
	return func(c *Config) {
		if n > 0 {
			c.AcceptBacklog = n
		}
	}
}

// WithStreamWindowSize 设置每流窗口大小。
// WithStreamWindowSize sets the per-stream window size.
func WithStreamWindowSize(n uint32) Option {
	return func(c *Config) {
		if n > 0 {
			c.MaxStreamWindowSize = n
		}
	}
}

// WithKeepAlive 设置 keepalive 间隔（<=0 关闭）。
// WithKeepAlive sets the keepalive interval (<=0 disables it).
func WithKeepAlive(d time.Duration) Option {
	return func(c *Config) { c.KeepAliveInterval = d }
}

// WithConnectionWriteTimeout 设置写出与 keepalive 响应超时。
// WithConnectionWriteTimeout sets the frame write / keepalive response timeout.
func WithConnectionWriteTimeout(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.ConnectionWriteTimeout = d
		}
	}
}

// Muxer 是链路复用会话接口。
// Muxer is the multiplexing session interface.
type Muxer interface {
	OpenStream(ctx context.Context) (Stream, error)
	AcceptStream(ctx context.Context) (Stream, error)
	Ping() (time.Duration, error)
	NumStreams() int
	GoAway() error
	Close() error
	IsClosed() bool
}

// Session 是 Muxer 的默认实现（yamux 帧协议）。
// Session is the default Muxer implementation (yamux framing).
type Session struct {
	conn net.Conn
	cfg  Config

	writeMu sync.Mutex

	mu           sync.Mutex
	streams      map[uint32]*stream
	nextID       uint32
	backlog      chan *stream
	streamsEmpty chan struct{}
	shutdownFlag bool
	goAwayFlag   bool
	sessionErr   error

	pingMu sync.Mutex
	pingID uint32
	pings  map[uint32]*pendingPing

	closed    chan struct{}
	closeOnce sync.Once
}

// pendingPing 记录一次未完成的 ping。
// pendingPing tracks one outstanding ping.
type pendingPing struct {
	start time.Time
	ch    chan time.Duration
}

// New 在 conn 上创建复用会话并启动读循环。
// New creates a multiplexing session over conn and starts the read loop.
func New(conn net.Conn, opts ...Option) (*Session, error) {
	if conn == nil {
		return nil, errors.New("mux: nil conn")
	}
	cfg := Config{
		AcceptBacklog:          defaultAcceptBacklog,
		MaxStreamWindowSize:    defaultStreamWindow,
		KeepAliveInterval:      0, // 与 yamux 一致：默认关闭
		ConnectionWriteTimeout: defaultWriteTimeout,
	}
	for _, o := range opts {
		o(&cfg)
	}
	s := &Session{
		conn:    conn,
		cfg:     cfg,
		streams: make(map[uint32]*stream),
		nextID:  1, // 发起端流 ID 为奇数；对端为偶数
		backlog: make(chan *stream, cfg.AcceptBacklog),
		pings:   make(map[uint32]*pendingPing),
		closed:  make(chan struct{}),
	}
	go s.readLoop()
	go s.keepaliveLoop()
	return s, nil
}

// OpenStream 打开一条新流；ctx 取消则中止。go-away 后返回 ErrGoAway。
// OpenStream opens a new stream; ctx cancellation aborts. Returns ErrGoAway
// after go-away.
func (s *Session) OpenStream(ctx context.Context) (Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.shutdownFlag {
		s.mu.Unlock()
		return nil, ErrSessionShutdown
	}
	if s.goAwayFlag {
		s.mu.Unlock()
		return nil, ErrGoAway
	}
	if s.nextID > 0xfffffffe {
		s.mu.Unlock()
		return nil, ErrStreamsExhausted
	}
	id := s.nextID
	s.nextID += 2
	st := s.newStream(id)
	if len(s.streams) == 0 {
		s.streamsEmpty = make(chan struct{})
	}
	s.streams[id] = st
	s.mu.Unlock()

	// SYN 携带本端初始接收窗口。
	// The SYN carries our initial receive window.
	if err := s.sendFrame(header{Type: typeWindowUpdate, Flags: flagSYN, StreamID: id, Length: st.recvWindowValue()}, nil); err != nil {
		s.removeStream(st, err)
		return nil, err
	}
	return st, nil
}

// AcceptStream 接受对端打开的流；ctx 取消或会话关闭时返回错误。
// AcceptStream accepts a peer-opened stream; returns an error on ctx
// cancellation or session shutdown.
func (s *Session) AcceptStream(ctx context.Context) (Stream, error) {
	select {
	case st := <-s.backlog:
		if st == nil {
			return nil, ErrSessionShutdown
		}
		return st, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, s.shutdownErr()
	}
}

// Ping 测量 RTT：发送 ping 并等待对端回显。
// Ping measures RTT by sending a ping and awaiting the peer echo.
func (s *Session) Ping() (time.Duration, error) {
	ch, id, err := s.registerPing()
	if err != nil {
		return 0, err
	}
	defer s.unregisterPing(id)
	if err := s.sendFrame(header{Type: typePing, Flags: flagSYN, Length: id}, nil); err != nil {
		return 0, err
	}
	select {
	case rtt := <-ch:
		return rtt, nil
	case <-s.closed:
		return 0, s.shutdownErr()
	}
}

// NumStreams 返回当前活跃流数量。
// NumStreams returns the number of active streams.
func (s *Session) NumStreams() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

// GoAway 拒绝对端新流并停止本端开流；存量流继续。
// GoAway rejects new peer streams and stops local opens; existing streams
// continue.
func (s *Session) GoAway() error {
	s.mu.Lock()
	if s.goAwayFlag || s.shutdownFlag {
		s.mu.Unlock()
		return nil
	}
	s.goAwayFlag = true
	s.mu.Unlock()
	return s.sendFrame(header{Type: typeGoAway}, nil)
}

// Close 优雅关闭：go-away 后等待存量流排空（至多 ConnectionWriteTimeout），
// 再关闭底层连接。
// Close gracefully shuts down: go-away, drain existing streams (bounded by
// ConnectionWriteTimeout), then close the transport.
func (s *Session) Close() error {
	_ = s.GoAway()
	s.mu.Lock()
	empty := len(s.streams) == 0
	var drained <-chan struct{}
	if !empty {
		drained = s.streamsEmpty
	}
	s.mu.Unlock()
	if !empty {
		select {
		case <-drained:
		case <-time.After(s.cfg.ConnectionWriteTimeout):
		}
	}
	s.shutdown(nil)
	return nil
}

// IsClosed 报告会话是否已关闭。
// IsClosed reports whether the session is closed.
func (s *Session) IsClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// shutdownErr 返回会话错误（无则 ErrSessionShutdown）。
// shutdownErr returns the session error (ErrSessionShutdown when absent).
func (s *Session) shutdownErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionErr != nil {
		return s.sessionErr
	}
	return ErrSessionShutdown
}

// sendFrame 写出一个完整帧（头+体一次写）。
// sendFrame writes one complete frame (header+body in one write).
func (s *Session) sendFrame(h header, body []byte) error {
	frame := make([]byte, headerSize+len(body))
	h.encode(frame)
	copy(frame[headerSize:], body)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.cfg.ConnectionWriteTimeout > 0 {
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.cfg.ConnectionWriteTimeout))
		defer func() { _ = s.conn.SetWriteDeadline(time.Time{}) }()
	}
	for len(frame) > 0 {
		n, err := s.conn.Write(frame)
		if err != nil {
			s.shutdown(err)
			return err
		}
		frame = frame[n:]
	}
	return nil
}

// readLoop 读帧并分发。
// readLoop reads and dispatches frames.
func (s *Session) readLoop() {
	var hbuf [headerSize]byte
	for {
		if _, err := io.ReadFull(s.conn, hbuf[:]); err != nil {
			s.shutdown(err)
			return
		}
		h := decodeHeader(hbuf[:])
		var body []byte
		// 仅 Data 帧携带 body；WindowUpdate/Ping/GoAway 的 Length 是
		// 窗口增量/回显值/错误码，不是负载长度。
		// Only Data frames carry a body; for WindowUpdate/Ping/GoAway the
		// Length field is a window delta / echo value / error code.
		if h.Type == typeData && h.Length > 0 {
			body = make([]byte, h.Length)
			if _, err := io.ReadFull(s.conn, body); err != nil {
				s.shutdown(err)
				return
			}
		}
		if err := s.dispatch(h, body); err != nil {
			s.shutdown(err)
			return
		}
	}
}

// dispatch 处理一帧。
// dispatch handles one frame.
func (s *Session) dispatch(h header, body []byte) error {
	switch h.Type {
	case typeData:
		st := s.getStream(h.StreamID)
		if h.Flags&flagRST != 0 {
			if st != nil {
				st.recvRST()
			}
			return nil
		}
		if st == nil {
			if h.Flags&flagSYN != 0 {
				// 数据帧 SYN 的 Length 是数据长度，初始窗口保持默认，
				// 以随后的 WindowUpdate 帧为准。
				// A data-frame SYN carries the data length in Length; the
				// initial window stays default until a WindowUpdate arrives.
				st = s.acceptStream(h.StreamID, 0)
				if st == nil {
					return nil // go-away 拒绝：已回 RST
				}
			} else {
				// 未知流：回 RST。
				_ = s.sendFrame(header{Type: typeData, Flags: flagRST, StreamID: h.StreamID}, nil)
				return nil
			}
		}
		if h.Flags&flagFIN != 0 {
			st.recvFIN()
		}
		if len(body) > 0 {
			return st.recvData(body)
		}
		return nil
	case typeWindowUpdate:
		st := s.getStream(h.StreamID)
		if st == nil {
			if h.Flags&flagSYN != 0 {
				if s.acceptStream(h.StreamID, h.Length) == nil {
					return nil
				}
				return nil
			}
			return nil
		}
		st.recvWindowUpdate(h)
		return nil
	case typePing:
		if h.Flags&flagACK != 0 {
			s.resolvePing(h.Length)
			return nil
		}
		// 对端 ping：回显。
		return s.sendFrame(header{Type: typePing, Flags: flagACK, Length: h.Length}, nil)
	case typeGoAway:
		s.mu.Lock()
		s.goAwayFlag = true
		s.mu.Unlock()
		if h.Length > 0 {
			return errors.New("mux: remote error")
		}
		return nil
	default:
		return errors.New("mux: unknown frame type")
	}
}

// getStream 读取流。
// getStream fetches a stream.
func (s *Session) getStream(id uint32) *stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[id]
}

// acceptStream 处理对端 SYN 开流；initialWindow>0 时设置对端通告的初始
// 发送窗口。go-away 时回 RST 并返回 nil。
//
// acceptStream handles a peer SYN open; initialWindow>0 sets the
// peer-advertised initial send window. On go-away it replies RST and returns
// nil.
func (s *Session) acceptStream(id uint32, initialWindow uint32) *stream {
	s.mu.Lock()
	if s.goAwayFlag || s.shutdownFlag || s.streams[id] != nil {
		s.mu.Unlock()
		if s.streams[id] == nil {
			_ = s.sendFrame(header{Type: typeData, Flags: flagRST, StreamID: id}, nil)
		}
		return nil
	}
	st := s.newStream(id)
	if initialWindow > 0 {
		st.setSendWindow(initialWindow)
	}
	if len(s.streams) == 0 {
		s.streamsEmpty = make(chan struct{})
	}
	s.streams[id] = st
	backlog := s.backlog
	s.mu.Unlock()

	// ACK 携带本端接收窗口。
	// The ACK carries our receive window.
	_ = s.sendFrame(header{Type: typeWindowUpdate, Flags: flagACK, StreamID: id, Length: st.recvWindowValue()}, nil)

	select {
	case backlog <- st:
	default:
		// backlog 满：拒流。
		_ = s.sendFrame(header{Type: typeData, Flags: flagRST, StreamID: id}, nil)
		s.removeStream(st, ErrSessionShutdown)
		return nil
	}
	return st
}

// newStream 构造流（调用方须持 s.mu）。
// newStream constructs a stream (caller must hold s.mu).
func (s *Session) newStream(id uint32) *stream {
	return &stream{
		session:    s,
		id:         id,
		recvWindow: s.cfg.MaxStreamWindowSize,
		sendWindow: s.cfg.MaxStreamWindowSize,
		readWait:   make(chan struct{}, 1),
		writeWait:  make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
}

// removeStream 从会话移除流；流表为空时唤醒排空等待者。
// removeStream removes a stream; wakes drain waiters when the table empties.
func (s *Session) removeStream(st *stream, err error) {
	st.markDead()
	s.mu.Lock()
	if _, ok := s.streams[st.id]; ok {
		delete(s.streams, st.id)
		if len(s.streams) == 0 && s.streamsEmpty != nil {
			close(s.streamsEmpty)
			s.streamsEmpty = nil
		}
	}
	s.mu.Unlock()
}

// shutdown 关闭会话：关闭底层连接、唤醒全部等待者。
// shutdown closes the session: transport close and all waiters woken.
func (s *Session) shutdown(err error) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.shutdownFlag = true
		if err != nil && s.sessionErr == nil {
			s.sessionErr = err
		}
		streams := make([]*stream, 0, len(s.streams))
		for _, st := range s.streams {
			streams = append(streams, st)
		}
		s.mu.Unlock()
		close(s.closed)
		_ = s.conn.Close()
		for _, st := range streams {
			st.sessionClosed()
		}
	})
}

// keepaliveLoop 周期性 ping；超时未响应则关闭会话。
// keepaliveLoop pings periodically; shuts down on response timeout.
func (s *Session) keepaliveLoop() {
	if s.cfg.KeepAliveInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.cfg.KeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
		}
		ch, id, err := s.registerPing()
		if err != nil {
			return
		}
		if err := s.sendFrame(header{Type: typePing, Flags: flagSYN, Length: id}, nil); err != nil {
			s.unregisterPing(id)
			return
		}
		select {
		case <-ch:
			s.unregisterPing(id)
		case <-time.After(s.cfg.ConnectionWriteTimeout):
			s.unregisterPing(id)
			s.shutdown(ErrKeepAliveTimeout)
			return
		case <-s.closed:
			s.unregisterPing(id)
			return
		}
	}
}

// registerPing 登记一次 ping。
// registerPing registers a ping.
func (s *Session) registerPing() (chan time.Duration, uint32, error) {
	s.pingMu.Lock()
	defer s.pingMu.Unlock()
	select {
	case <-s.closed:
		return nil, 0, s.shutdownErr()
	default:
	}
	id := s.pingID
	s.pingID++
	ch := make(chan time.Duration, 1)
	s.pings[id] = &pendingPing{start: time.Now(), ch: ch}
	return ch, id, nil
}

// unregisterPing 注销一次 ping。
// unregisterPing removes a ping.
func (s *Session) unregisterPing(id uint32) {
	s.pingMu.Lock()
	delete(s.pings, id)
	s.pingMu.Unlock()
}

// resolvePing 结算 ping 响应。
// resolvePing resolves a ping response.
func (s *Session) resolvePing(id uint32) {
	s.pingMu.Lock()
	p, ok := s.pings[id]
	if ok {
		delete(s.pings, id)
	}
	s.pingMu.Unlock()
	if ok {
		p.ch <- time.Since(p.start)
	}
}

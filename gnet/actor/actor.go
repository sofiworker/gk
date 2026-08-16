// Package actor 提供建立在 reactor Conn 之上的 actor 编排薄层：每个
// Worker 独占一条连接，消息（连接数据 + 外部投递）在单一 goroutine 上
// 串行处理，天然无锁。参照 zhenyi zactor 的"连接所有权 + 邮箱"模型。
//
// Package actor provides a thin actor orchestration layer over reactor
// Conns: each Worker owns one conn and processes messages (conn data plus
// external posts) serially on a single goroutine — lock-free by design.
// Modeled on zhenyi zactor's conn-ownership + mailbox pattern.
package actor

import (
	"sync"

	"github.com/sofiworker/gk/gnet"
)

// MessageHandler 在 actor goroutine 上串行处理消息。
// MessageHandler processes messages serially on the actor goroutine.
type MessageHandler interface {
	// OnMessage 处理一条消息并返回响应（可 nil）。
	// 注意：消息边界即一次连接 Read 的字节；协议分帧请用 codec 自行处理。
	// OnMessage handles one message and returns a response (may be nil).
	// Note: a message is one conn Read's bytes; use codec for framing.
	OnMessage(data []byte) []byte
	// OnClose 连接关闭回调（任何关闭路径都恰好触发一次）。
	// OnClose fires exactly once on every close path.
	OnClose(err error)
}

// Worker 是独占一条连接的 actor：连接数据与外部消息经邮箱在单一
// goroutine 上串行处理。
//
// Worker is an actor owning one conn: conn data and external posts flow
// through a mailbox processed serially on a single goroutine.
type Worker struct {
	conn    gnet.Conn
	handler MessageHandler
	mailbox chan []byte
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
}

// SpawnWorker 启动 worker（连接由 worker 独占）。
// SpawnWorker starts a worker (the conn is owned by the worker).
func SpawnWorker(conn gnet.Conn, h MessageHandler, mailboxSize int) *Worker {
	if mailboxSize <= 0 {
		mailboxSize = 256
	}
	w := &Worker{
		conn:    conn,
		handler: h,
		mailbox: make(chan []byte, mailboxSize),
		done:    make(chan struct{}),
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// Send 投递一条外部消息；邮箱满时丢弃并返回 false。
// Send posts an external message; drops it (returns false) when the mailbox
// is full.
func (w *Worker) Send(data []byte) bool {
	select {
	case w.mailbox <- data:
		return true
	case <-w.done:
		return false
	default:
		return false
	}
}

// Stop 停止 worker 并关闭连接；等待处理循环退出。
// Stop halts the worker, closes the conn, and waits for the loops to exit.
func (w *Worker) Stop() {
	w.shutdown(nil)
	w.wg.Wait()
}

// Conn 返回 worker 独占的连接。
// Conn returns the worker's conn.
func (w *Worker) Conn() gnet.Conn { return w.conn }

// shutdown 幂等关闭：关 done、关连接、触发一次 OnClose。
// shutdown idempotently closes: done, conn, and one OnClose.
func (w *Worker) shutdown(err error) {
	w.once.Do(func() {
		close(w.done)
		_ = w.conn.Close()
		w.handler.OnClose(err)
	})
}

// run 是邮箱处理循环；连接读由 readLoop 搬运进邮箱。
// run is the mailbox processing loop; conn reads are relayed by readLoop.
func (w *Worker) run() {
	defer w.wg.Done()
	w.wg.Add(1)
	go w.readLoop()
	for {
		select {
		case msg := <-w.mailbox:
			if resp := w.handler.OnMessage(msg); resp != nil {
				_, _ = w.conn.Write(resp)
			}
		case <-w.done:
			return
		}
	}
}

// readLoop 把连接数据搬进邮箱（拷贝，避免与读缓冲共享）。
// readLoop relays conn data into the mailbox (copied, never shared).
func (w *Worker) readLoop() {
	defer w.wg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := w.conn.Read(buf)
		if n > 0 {
			msg := append([]byte(nil), buf[:n]...)
			select {
			case w.mailbox <- msg:
			case <-w.done:
				return
			}
		}
		if err != nil {
			w.shutdown(err)
			return
		}
	}
}

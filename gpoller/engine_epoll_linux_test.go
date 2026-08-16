//go:build linux

package gpoller

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// newPair 创建一对相连的 socket。
func newPair(t *testing.T) (int, int) {
	t.Helper()
	var fds [2]int
	var err error
	fds, err = unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	t.Cleanup(func() {
		unix.Close(fds[0])
		unix.Close(fds[1])
	})
	return fds[0], fds[1]
}

// TestEpollEngineReadDispatch 覆盖读就绪事件分发。
func TestEpollEngineReadDispatch(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()

	done := make(chan struct{})
	go func() {
		_ = e.Poll()
		close(done)
	}()
	defer func() {
		e.Close()
		<-done
	}()

	a, b := newPair(t)
	got := make(chan Event, 1)
	if err := e.AddRead(a, func(ev Event) {
		if ev.Readable {
			// LT 语义：必须消费数据，否则事件会反复触发。
			var tmp [4]byte
			_, _ = unix.Read(a, tmp[:])
			got <- ev
		}
	}); err != nil {
		t.Fatalf("AddRead: %v", err)
	}

	if _, err := unix.Write(b, []byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case ev := <-got:
		if ev.FD != a {
			t.Fatalf("FD = %d, want %d", ev.FD, a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for read event")
	}
}

// TestEpollEngineWake 覆盖跨 goroutine 任务投递。
func TestEpollEngineWake(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()

	done := make(chan struct{})
	go func() {
		_ = e.Poll()
		close(done)
	}()
	defer func() {
		e.Close()
		<-done
	}()

	ran := make(chan int, 2)
	if err := e.Wake(func() { ran <- 1 }); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if err := e.Wake(func() { ran <- 2 }); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	for i := 1; i <= 2; i++ {
		select {
		case v := <-ran:
			if v != i {
				t.Fatalf("task order = %d, want %d", v, i)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for task %d", i)
		}
	}
}

// TestEpollEngineDeleteAndClose 覆盖注销与 Close 停止 Poll。
func TestEpollEngineDeleteAndClose(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a, b := newPair(t)
	var calls atomic.Int32
	if err := e.AddRead(a, func(ev Event) { calls.Add(1) }); err != nil {
		t.Fatalf("AddRead: %v", err)
	}
	if err := e.Delete(a); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := e.Mod(a, true, false); err != ErrNotRegistered {
		t.Fatalf("Mod after Delete = %v, want ErrNotRegistered", err)
	}
	if _, err := unix.Write(b, []byte("y")); err != nil {
		t.Fatalf("write: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_ = e.Poll()
		close(done)
	}()
	// 短暂等待后关闭，Poll 应退出。
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Poll did not exit after Close")
	}
	if calls.Load() != 0 {
		t.Fatalf("deleted fd dispatched %d events, want 0", calls.Load())
	}
	if err := e.AddRead(a, func(Event) {}); err != ErrClosed {
		t.Fatalf("AddRead after Close = %v, want ErrClosed", err)
	}
}

// TestEpollEngineConcurrentWake 覆盖并发 Wake 的安全性。
func TestEpollEngineConcurrentWake(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()

	done := make(chan struct{})
	go func() {
		_ = e.Poll()
		close(done)
	}()
	defer func() {
		e.Close()
		<-done
	}()

	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = e.Wake(func() { count.Add(1) })
			}
		}()
	}
	wg.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for count.Load() < 400 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := count.Load(); got != 400 {
		t.Fatalf("executed %d tasks, want 400", got)
	}
}

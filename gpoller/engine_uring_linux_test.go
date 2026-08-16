//go:build linux

package gpoller

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// newUring 创建引擎；环境不支持（seccomp/老内核）时跳过。
func newUring(t *testing.T) CompletionEngine {
	t.Helper()
	e, err := NewCompletionEngine()
	if err != nil {
		t.Skipf("io_uring unavailable: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

// TestUringWriteRead 覆盖写与读完成回调。
func TestUringWriteRead(t *testing.T) {
	e := newUring(t)
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(p[0])
	defer unix.Close(p[1])

	// 写。
	type res struct {
		n   int
		err error
	}
	writeCh := make(chan res, 1)
	payload := []byte("hello io_uring")
	if _, err := e.Submit(Op{Kind: OpWrite, FD: p[1], Buf: payload}, func(r Result) {
		writeCh <- res{r.N, r.Err}
	}); err != nil {
		t.Fatalf("Submit write: %v", err)
	}
	wr := <-writeCh
	if wr.err != nil || wr.n != len(payload) {
		t.Fatalf("write result = %+v", wr)
	}

	// 读。
	readCh := make(chan res, 1)
	buf := make([]byte, len(payload))
	if _, err := e.Submit(Op{Kind: OpRead, FD: p[0], Buf: buf}, func(r Result) {
		readCh <- res{r.N, r.Err}
	}); err != nil {
		t.Fatalf("Submit read: %v", err)
	}
	rr := <-readCh
	if rr.err != nil || rr.n != len(payload) || string(buf) != string(payload) {
		t.Fatalf("read result = %+v, buf=%q", rr, buf)
	}
}

// TestUringNop 覆盖 NOP 立即完成。
func TestUringNop(t *testing.T) {
	e := newUring(t)
	ch := make(chan Result, 1)
	if _, err := e.Submit(Op{Kind: OpTimeout}, func(r Result) { ch <- r }); err != nil {
		t.Fatalf("Submit nop: %v", err)
	}
	select {
	case r := <-ch:
		if r.Err != nil || r.N != 0 {
			t.Fatalf("nop result = %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nop did not complete")
	}
}

// TestUringReadBlocking 覆盖读空管道阻塞直至数据到达。
func TestUringReadBlocking(t *testing.T) {
	e := newUring(t)
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(p[0])
	defer unix.Close(p[1])

	readCh := make(chan Result, 1)
	buf := make([]byte, 4)
	if _, err := e.Submit(Op{Kind: OpRead, FD: p[0], Buf: buf}, func(r Result) {
		readCh <- r
	}); err != nil {
		t.Fatal(err)
	}
	// 确认先阻塞。
	select {
	case r := <-readCh:
		t.Fatalf("read completed early: %+v", r)
	case <-time.After(80 * time.Millisecond):
	}
	_, _ = unix.Write(p[1], []byte("ping"))
	select {
	case r := <-readCh:
		if r.Err != nil || r.N != 4 || string(buf) != "ping" {
			t.Fatalf("read result = %+v buf=%q", r, buf)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read did not complete after data")
	}
}

// TestUringCancel 覆盖取消未完成的读。
func TestUringCancel(t *testing.T) {
	e := newUring(t)
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(p[0])
	defer unix.Close(p[1])

	readCh := make(chan Result, 1)
	buf := make([]byte, 4)
	id, err := e.Submit(Op{Kind: OpRead, FD: p[0], Buf: buf}, func(r Result) {
		readCh <- r
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := e.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	select {
	case r := <-readCh:
		if r.Err == nil || r.Err != os.ErrDeadlineExceeded && r.Err.Error() == "" {
			// 取消的典型结果是 -ECANCELED；接受任意负 errno。
			if r.Err == nil {
				t.Fatalf("cancelled read = %+v, want error", r)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not complete")
	}
}

// TestUringUnsupportedOps 覆盖不支持的 OpKind。
func TestUringUnsupportedOps(t *testing.T) {
	e := newUring(t)
	if _, err := e.Submit(Op{Kind: OpAccept}, func(Result) {}); err == nil {
		t.Fatal("Submit(Accept) = nil error")
	}
	if _, err := e.Submit(Op{Kind: OpConnect}, func(Result) {}); err == nil {
		t.Fatal("Submit(Connect) = nil error")
	}
}

// TestUringCloseIdempotent 覆盖重复 Close。
func TestUringCloseIdempotent(t *testing.T) {
	e := newUring(t)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if _, err := e.Submit(Op{Kind: OpTimeout}, func(Result) {}); err != ErrClosed {
		t.Fatalf("Submit after Close = %v, want ErrClosed", err)
	}
}

// BenchmarkUringPipe 基准：io_uring 在管道上的读写往返吞吐。
// BenchmarkUringPipe benchmarks io_uring read/write round trips over a pipe.
func BenchmarkUringPipe(b *testing.B) {
	e, err := NewCompletionEngine()
	if err != nil {
		b.Skipf("io_uring unavailable: %v", err)
	}
	defer e.Close()
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC); err != nil {
		b.Fatal(err)
	}
	defer unix.Close(p[0])
	defer unix.Close(p[1])

	payload := make([]byte, 4096)
	buf := make([]byte, 4096)
	done := make(chan struct{}, 2)
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Submit(Op{Kind: OpWrite, FD: p[1], Buf: payload}, func(Result) {
			select {
			case done <- struct{}{}:
			default:
			}
		})
		if err != nil {
			b.Fatal(err)
		}
		<-done
		_, err = e.Submit(Op{Kind: OpRead, FD: p[0], Buf: buf}, func(Result) {
			select {
			case done <- struct{}{}:
			default:
			}
		})
		if err != nil {
			b.Fatal(err)
		}
		<-done
	}
}

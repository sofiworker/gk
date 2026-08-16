package gpoller

import (
	"sync"
	"testing"
	"time"
)

// TestTimingWheelFireOrder 覆盖到期顺序。
func TestTimingWheelFireOrder(t *testing.T) {
	w := NewTimingWheel(WithTickDuration(2*time.Millisecond), WithTicksPerWheel(16))
	defer w.Stop()

	var mu sync.Mutex
	var got []int
	fire := func(v int) func() {
		return func() {
			mu.Lock()
			got = append(got, v)
			mu.Unlock()
		}
	}
	w.Add(12*time.Millisecond, fire(3))
	w.Add(2*time.Millisecond, fire(1))
	w.Add(8*time.Millisecond, fire(2))

	waitFor(t, &mu, &got, 3, 2*time.Second)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("fire order = %v, want [1 2 3]", got)
	}
}

// TestTimingWheelCancel 覆盖取消与重复取消。
func TestTimingWheelCancel(t *testing.T) {
	w := NewTimingWheel(WithTickDuration(2*time.Millisecond), WithTicksPerWheel(16))
	defer w.Stop()

	fired := make(chan int, 4)
	id := w.Add(6*time.Millisecond, func() { fired <- 1 })
	if !w.Cancel(id) {
		t.Fatal("first Cancel = false, want true")
	}
	if w.Cancel(id) {
		t.Fatal("second Cancel = true, want false")
	}
	w.Add(4*time.Millisecond, func() { fired <- 2 })
	select {
	case v := <-fired:
		if v != 2 {
			t.Fatalf("fired = %d, want 2 (cancelled timer must not fire)", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for uncancelled timer")
	}
	select {
	case v := <-fired:
		t.Fatalf("cancelled timer fired: %d", v)
	case <-time.After(20 * time.Millisecond):
	}
}

// TestTimingWheelRounds 覆盖超过一轮的延迟（轮转计数）。
func TestTimingWheelRounds(t *testing.T) {
	w := NewTimingWheel(WithTickDuration(time.Millisecond), WithTicksPerWheel(8))
	defer w.Stop()

	start := time.Now()
	fired := make(chan time.Duration, 1)
	w.Add(25*time.Millisecond, func() { fired <- time.Since(start) })
	select {
	case d := <-fired:
		if d < 20*time.Millisecond {
			t.Fatalf("fired too early: %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for multi-round timer")
	}
}

// TestTimingWheelStop 覆盖停止后 Add 返回 0。
func TestTimingWheelStop(t *testing.T) {
	w := NewTimingWheel(WithTickDuration(time.Millisecond))
	w.Stop()
	w.Stop() // 幂等
	if id := w.Add(time.Second, func() {}); id != 0 {
		t.Fatalf("Add after Stop = %d, want 0", id)
	}
}

// waitFor 轮询等待 got 达到期望长度，避免慢 CI 下的睡眠抖动。
func waitFor(t *testing.T, mu *sync.Mutex, got *[]int, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		mu.Lock()
		n := len(*got)
		mu.Unlock()
		if n >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: got %d firings, want %d", n, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

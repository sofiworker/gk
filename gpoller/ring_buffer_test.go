package gpoller

import (
	"bytes"
	"testing"
)

// TestRingBufferWriteRead 覆盖写满、环绕读写与部分读写。
func TestRingBufferWriteRead(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		writes   []string
		reads    []int
		want     []string
	}{
		{"sequential", 16, []string{"hello", "world"}, []int{5, 5}, []string{"hello", "world"}},
		{"wraparound", 8, []string{"abcd", "efgh"}, []int{5, 3}, []string{"abcde", "fgh"}},
		{"overwrite-stop", 4, []string{"abcdef"}, []int{4}, []string{"abcd"}},
		{"partial-reads", 16, []string{"0123456789"}, []int{3, 3, 4}, []string{"012", "345", "6789"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewRingBuffer(tt.capacity)
			for _, w := range tt.writes {
				b.Write([]byte(w))
			}
			for i, rn := range tt.reads {
				got := make([]byte, rn)
				n := b.Read(got)
				if string(got[:n]) != tt.want[i] {
					t.Fatalf("read #%d = %q, want %q", i, got[:n], tt.want[i])
				}
			}
			if b.Len() != 0 {
				t.Fatalf("Len() = %d, want 0", b.Len())
			}
		})
	}
}

// TestRingBufferInterleaved 覆盖交错读写下的写侧环绕。
func TestRingBufferInterleaved(t *testing.T) {
	b := NewRingBuffer(8)
	b.Write([]byte("abcde"))
	if got := readAll(b, 3); got != "abc" {
		t.Fatalf("first read = %q, want abc", got)
	}
	b.Write([]byte("fghij")) // tail 环绕：fgh 落尾部，ij 落头部
	if got := readAll(b, 7); got != "defghij" {
		t.Fatalf("second read = %q, want defghij", got)
	}
	if b.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", b.Len())
	}
}

// readAll 一次读满 n 字节。
func readAll(b *RingBuffer, n int) string {
	buf := make([]byte, n)
	m := b.Read(buf)
	return string(buf[:m])
}

// TestRingBufferPeekDiscard 覆盖 Peek 视图与 Discard。
func TestRingBufferPeekDiscard(t *testing.T) {
	b := NewRingBuffer(8)
	b.Write([]byte("abcdef"))
	if got := string(b.Peek(3)); got != "abc" {
		t.Fatalf("Peek(3) = %q, want abc", got)
	}
	if b.Len() != 6 {
		t.Fatalf("Peek consumed data: Len() = %d, want 6", b.Len())
	}
	// Peek 环绕时只返回首段连续视图。
	b.Discard(6)
	b.Write([]byte("abcdefgh"))
	if got := string(b.Peek(100)); len(got) != 2 {
		t.Fatalf("wrapped Peek len = %d, want 2 (head segment)", len(got))
	}
	b.Discard(2)
	b.Discard(100)
	if b.Len() != 0 {
		t.Fatalf("Len() after discards = %d, want 0", b.Len())
	}
}

// TestRingBufferReset 覆盖 Reset。
func TestRingBufferReset(t *testing.T) {
	b := NewRingBuffer(4)
	b.Write([]byte("ab"))
	b.Reset()
	if b.Len() != 0 || b.Cap() != 4 {
		t.Fatalf("after Reset: Len=%d Cap=%d, want 0/4", b.Len(), b.Cap())
	}
	b.Write([]byte("xy"))
	if got := string(b.Peek(2)); got != "xy" {
		t.Fatalf("after Reset write = %q, want xy", got)
	}
}

// TestRingBufferLargeRoundTrip 用随机数据做全量回环。
func TestRingBufferLargeRoundTrip(t *testing.T) {
	const cap, total = 37, 10000
	b := NewRingBuffer(cap)
	want := make([]byte, total)
	for i := range want {
		want[i] = byte(i * 31)
	}
	var out bytes.Buffer
	for off := 0; off < total; {
		n := b.Write(want[off:])
		off += n
		if off < total && n == 0 {
			buf := make([]byte, 17)
			m := b.Read(buf)
			if m == 0 {
				t.Fatal("no progress")
			}
			out.Write(buf[:m])
		}
	}
	for b.Len() > 0 {
		buf := make([]byte, 17)
		n := b.Read(buf)
		out.Write(buf[:n])
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatal("round trip mismatch")
	}
}

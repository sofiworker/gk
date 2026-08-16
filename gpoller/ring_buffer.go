package gpoller

// RingBuffer 是定长环形字节缓冲：写满即止（不扩容、不覆盖），
// 读写均 O(1)，适合作为连接级入站缓冲与事件循环的临时缓冲。
//
// RingBuffer is a fixed-capacity circular byte buffer: writes stop when full
// (no growth, no overwrite); reads and writes are O(1). It suits per-conn
// inbound buffering and event-loop scratch buffers.
type RingBuffer struct {
	buf  []byte
	head int // 读位置；read position
	tail int // 写位置；write position
	size int // 当前长度；current length
}

// NewRingBuffer 创建容量为 capacity 的环形缓冲（最小 1）。
// NewRingBuffer creates a ring buffer with the given capacity (at least 1).
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{buf: make([]byte, capacity)}
}

// Len 返回当前已缓冲的字节数。
// Len returns the number of buffered bytes.
func (b *RingBuffer) Len() int { return b.size }

// Cap 返回容量。
// Cap returns the capacity.
func (b *RingBuffer) Cap() int { return len(b.buf) }

// Write 写入数据，写满为止，返回实际写入字节数。
// Write writes data until full and returns the number of bytes written.
func (b *RingBuffer) Write(p []byte) int {
	if len(p) == 0 || b.size == len(b.buf) {
		return 0
	}
	n := 0
	for n < len(p) && b.size < len(b.buf) {
		room := len(b.buf) - b.tail
		cnt := len(p) - n
		if cnt > room {
			cnt = room
		}
		if cnt > len(b.buf)-b.size {
			cnt = len(b.buf) - b.size
		}
		copy(b.buf[b.tail:], p[n:n+cnt])
		b.tail = (b.tail + cnt) % len(b.buf)
		b.size += cnt
		n += cnt
	}
	return n
}

// Read 读取至多 len(p) 字节，返回实际读取字节数。
// Read reads up to len(p) bytes and returns the number of bytes read.
func (b *RingBuffer) Read(p []byte) int {
	if len(p) == 0 || b.size == 0 {
		return 0
	}
	n := 0
	for n < len(p) && b.size > 0 {
		avail := len(b.buf) - b.head
		cnt := len(p) - n
		if cnt > avail {
			cnt = avail
		}
		if cnt > b.size {
			cnt = b.size
		}
		copy(p[n:n+cnt], b.buf[b.head:b.head+cnt])
		b.head = (b.head + cnt) % len(b.buf)
		b.size -= cnt
		n += cnt
	}
	return n
}

// Peek 返回至多 n 字节的只读视图（首个连续段，不消费数据）。
// Peek returns a read-only view of up to n bytes (first contiguous segment,
// without consuming).
func (b *RingBuffer) Peek(n int) []byte {
	if b.size == 0 || n <= 0 {
		return nil
	}
	if n > b.size {
		n = b.size
	}
	if avail := len(b.buf) - b.head; n > avail {
		n = avail
	}
	return b.buf[b.head : b.head+n]
}

// Discard 丢弃 n 字节。
// Discard drops n bytes.
func (b *RingBuffer) Discard(n int) {
	if n <= 0 {
		return
	}
	if n > b.size {
		n = b.size
	}
	b.head = (b.head + n) % len(b.buf)
	b.size -= n
}

// Reset 清空缓冲（不释放底层内存）。
// Reset clears the buffer (keeping the backing memory).
func (b *RingBuffer) Reset() {
	b.head, b.tail, b.size = 0, 0, 0
}

package ghttp

import (
	"errors"
	"io"
	"net/http"
	"sync"
)

// memoBody 把请求体包装成“首次访问才读取、之后共享字节”的缓冲。
// memoBody wraps the request body into a read-once buffer whose bytes are
// shared by every later consumer.
// 直接 Read 保持透传语义并顺手记录字节;bytes 负责把剩余流读完一次。
// direct Read stays pass-through while recording bytes; bytes drains the
// remainder exactly once.
type memoBody struct {
	src io.ReadCloser

	mu   sync.Mutex
	buf  []byte
	done bool
	err  error
	// limit 是路由解析后写入的有效 MaxBodyBytes;0 表示不限制。
	// limit is the effective MaxBodyBytes set after route resolution; 0 = no limit.
	limit int64
}

func (m *memoBody) Read(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done {
		return 0, io.EOF
	}
	n, err := m.src.Read(p)
	if n > 0 {
		if m.limit > 0 {
			remain := m.limit - int64(len(m.buf))
			if remain <= 0 {
				m.done = true
				m.err = &http.MaxBytesError{Limit: m.limit}
				return 0, m.err
			}
			if int64(n) > remain {
				m.buf = append(m.buf, p[:remain]...)
				m.done = true
				m.err = &http.MaxBytesError{Limit: m.limit}
				return int(remain), m.err
			}
		}
		m.buf = append(m.buf, p[:n]...)
	}
	if err != nil {
		m.done = true
		m.err = err
	}
	return n, err
}

func (m *memoBody) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done = true
	if m.err == nil {
		m.err = io.EOF
	}
	return m.src.Close()
}

// setLimit 设置字节上限,仅对后续 drain 生效。
// setLimit sets the byte cap; it only affects later drains.
func (m *memoBody) setLimit(n int64) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	m.limit = n
	m.mu.Unlock()
}

// drained 报告底层流是否已被整读。
// drained reports whether the underlying stream has been fully consumed.
func (m *memoBody) drained() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.done
}

// checkLimit 报告已缓冲字节是否超过设限(设限前被 middleware 整读的场景)。
// checkLimit reports whether buffered bytes exceed the cap (for bodies fully
// consumed by middleware before the cap was set).
func (m *memoBody) checkLimit() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.limit > 0 && int64(len(m.buf)) > m.limit {
		return &http.MaxBytesError{Limit: m.limit}
	}
	return nil
}

// bytes 返回完整请求体字节,第一次调用时读完剩余流。
// bytes returns the full body bytes, draining the remainder on first call.
func (m *memoBody) bytes() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.limit > 0 && int64(len(m.buf)) > m.limit {
		return nil, &http.MaxBytesError{Limit: m.limit}
	}
	if !m.done {
		// 渐进缓冲:小请求体只付小分配,大请求体逐步扩容,上限 32KB。
		// 固定 32KB 缓冲会让 180B 的典型 JSON body 每请求付出 32KB 分配
		// (实测 JSONBind 场景 94.6% 的分配字节来自此处),采用 io.ReadAll
		// 同款 512B 起倍增策略后分配降 91-93%。
		// grow the buffer incrementally: small bodies pay a small allocation
		// and large bodies expand on demand, capped at 32KB. A fixed 32KB
		// scratch made a typical 180B JSON body pay a 32KB allocation per
		// request (94.6% of JSONBind bytes in pprof); the io.ReadAll-style
		// 512B-doubling strategy cuts that by 91-93%.
		buf := make([]byte, 0, 512)
		for {
			if len(buf) == cap(buf) {
				next := cap(buf) * 2
				if next > 32*1024 {
					next = 32 * 1024
				}
				if next <= cap(buf) {
					next = cap(buf) + 4096
				}
				buf = append(buf, 0)[:len(buf):next]
			}
			prev := len(buf)
			n, err := m.src.Read(buf[prev:cap(buf)])
			buf = buf[:prev+n]
			if n > 0 && m.limit > 0 && int64(len(m.buf)+prev+n) > m.limit {
				// 丢弃本轮读取的字节,与固定缓冲版语义一致。
				// drop this round's bytes, matching the fixed-buffer semantics.
				m.done = true
				m.err = &http.MaxBytesError{Limit: m.limit}
				buf = buf[:prev]
				break
			}
			if err != nil {
				m.done = true
				m.err = err
				break
			}
		}
		// 空缓冲时直接转移所有权,避免 append 的额外拷贝。
		// transfer ownership when the memo is empty, skipping the append copy.
		if len(m.buf) == 0 {
			m.buf = buf
		} else {
			m.buf = append(m.buf, buf...)
		}
	}
	if m.err != nil && !errors.Is(m.err, io.EOF) {
		return m.buf, m.err
	}
	return m.buf, nil
}

// RawBody 返回请求体原始字节;首次访问读流并缓存到 requestState,之后所有
// 调用方(Params.RawBody、Body[T].Raw/Decode)共享同一份,不再读流。
// RawBody returns the raw body bytes; the first call reads the stream into the
// requestState memo, and later consumers (Params.RawBody, Body[T].Raw/Decode)
// share the same bytes without reading the stream again.
// 未经过 ghttp 派发链的请求直接读一次,不缓存。
// requests outside the ghttp dispatch chain are read once without caching.
func RawBody(r *http.Request) ([]byte, error) {
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	if st := requestStateFromRequest(r); st != nil && st.body != nil {
		return st.body.bytes()
	}
	return io.ReadAll(r.Body)
}

package ghttp

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ===========================================================================
// Server-Sent Events (SSE):在 RawHandle 之上的薄语法糖。
// SSEWriter 把一个 *Response 变成一条 text/event-stream 长连接:构造时写好 SSE 必需的
// 响应头,之后每次 Send*/Comment 都按 SSE 线格式编码并【立即 Flush】,让事件即时到达
// 客户端而非滞留缓冲。它不接管连接(SSE 是纯 HTTP,无需 Hijack),因此仍受中间件/优雅
// 关闭管理;写出错误(客户端断开常见)被记住,循环可据 Err() 或返回值退出。
// Server-Sent Events (SSE): a thin sugar layer over RawHandle. SSEWriter turns a
// *Response into a text/event-stream long-lived connection: it writes the SSE
// response headers at construction, then each Send*/Comment encodes one SSE wire
// record and Flushes immediately so events reach the client promptly instead of
// buffering. It does not hijack the connection (SSE is plain HTTP), so it stays
// under middleware and graceful shutdown; a write error (a client disconnect is
// common) is remembered so a loop can exit via Err() or the return value.
// ===========================================================================

// SSEMessage 是一条完整的 SSE 事件,承载 SSE 规范的四个字段。零值即一条只有 data 为空
// 的默认(message)事件。多行 Data 会按规范逐行加 "data:" 前缀。
// SSEMessage is one complete SSE event carrying the spec's four fields. The zero
// value is a default (message) event with empty data. Multi-line Data is emitted
// with a "data:" prefix per line as the spec requires.
type SSEMessage struct {
	// ID 事件 id(SSE "id:" 字段);非空时客户端会用它更新 Last-Event-ID,断线重连时
	// 经 Last-Event-ID 请求头回传,供服务端续传。空则不写该字段。
	// ID is the event id (SSE "id:"); when non-empty the client updates its
	// Last-Event-ID, sent back via the Last-Event-ID header on reconnect for
	// resuming. Empty omits the field.
	ID string
	// Event 事件类型(SSE "event:" 字段);空则为默认的 "message" 事件,客户端用
	// onmessage 接收,否则用 addEventListener(Event) 接收。
	// Event is the event type (SSE "event:"); empty means the default "message"
	// event received via onmessage, otherwise via addEventListener(Event).
	Event string
	// Data 事件数据(SSE "data:" 字段);含换行时按行拆分,每行独立加前缀。
	// Data is the event payload (SSE "data:"); newlines split it into per-line
	// prefixed fields.
	Data string
	// Retry 客户端断线后的重连等待(SSE "retry:" 字段,毫秒);<=0 则不写。
	// Retry is the client's reconnection delay after a disconnect (SSE "retry:",
	// milliseconds); <=0 omits the field.
	Retry time.Duration
}

// SSEWriter 是一条 SSE 事件流的写入器。经 NewSSEWriter 构造(自动写好响应头)。
// 非并发安全:同一时刻只应有一个 goroutine 写它。
// SSEWriter writes one SSE event stream. Construct it via NewSSEWriter (which
// writes the response headers). Not concurrency-safe: only one goroutine should
// write it at a time.
type SSEWriter struct {
	resp *Response
	buf  []byte
	err  error
}

// NewSSEWriter 把 resp 转为 SSE 写入器:设置 Content-Type: text/event-stream 与
// Cache-Control: no-cache 并提交 200(经 Flush 冲刷响应头),随后即可 Send。若底层
// writer 不支持 Flush(无法即时推送),事件仍会写出但可能被缓冲——这在标准 net/http
// 服务器上不会发生。返回的 *SSEWriter 的 Err() 在此之后反映写状态。
// NewSSEWriter turns resp into an SSE writer: it sets Content-Type
// text/event-stream and Cache-Control no-cache, commits 200 (flushing headers),
// then is ready to Send. If the underlying writer cannot Flush, events still write
// but may buffer — which does not happen on a standard net/http server.
func NewSSEWriter(resp *Response) *SSEWriter {
	h := resp.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// 禁用部分反向代理(如 nginx)的响应缓冲,保证事件不被代理层攒批。
	// Disable response buffering in some reverse proxies (e.g. nginx) so events
	// are not batched by the proxy layer.
	h.Set("X-Accel-Buffering", "no")
	resp.WriteHeader(http.StatusOK)
	resp.Flush()
	return &SSEWriter{resp: resp}
}

// Send 发送一条默认(message)事件,data 作为负载。等价于 SendMessage(SSEMessage{Data: data})。
// Send emits one default (message) event with data as the payload. Equivalent to
// SendMessage(SSEMessage{Data: data}).
func (w *SSEWriter) Send(data string) error {
	return w.SendMessage(SSEMessage{Data: data})
}

// SendEvent 发送一条具名事件(SSE "event:" 字段),客户端用 addEventListener(event) 接收。
// SendEvent emits one named event (SSE "event:"), received by the client via
// addEventListener(event).
func (w *SSEWriter) SendEvent(event, data string) error {
	return w.SendMessage(SSEMessage{Event: event, Data: data})
}

// SendMessage 按 SSE 线格式编码并发送一条完整事件,写完立即 Flush。已发生写错误时直接
// 返回该错误、不再写(避免对已断开的连接反复写)。
// SendMessage encodes one complete event in SSE wire format, sends it, and Flushes
// immediately. If a write error already occurred it returns that error without
// writing again (avoiding repeated writes to a broken connection).
func (w *SSEWriter) SendMessage(m SSEMessage) error {
	if w.err != nil {
		return w.err
	}
	w.buf = w.buf[:0]
	if m.ID != "" {
		w.buf = appendSSEField(w.buf, "id", stripSSELineBreaks(m.ID))
	}
	if m.Event != "" {
		w.buf = appendSSEField(w.buf, "event", stripSSELineBreaks(m.Event))
	}
	if m.Retry > 0 {
		w.buf = append(w.buf, "retry:"...)
		w.buf = strconv.AppendInt(w.buf, int64(m.Retry/time.Millisecond), 10)
		w.buf = append(w.buf, '\n')
	}
	// data 可为多行:按行拆分,每行一个 "data:" 字段;空 data 也写一个空 data 行,
	// 使其成为一条合法事件。SSE 的行终止符是 \n、\r\n 或【单独的 \r】,三者都必须拆行——
	// 只按 \n 拆会让裸 \r 原样进入 data 值,接收端据此断行,值的后半段就成了伪造字段。
	// data may be multi-line: split per line into "data:" fields; empty data still
	// writes one empty data line to form a valid event. An SSE line terminator is \n,
	// \r\n, or a LONE \r, and all three must split — splitting on \n alone would let a
	// bare \r reach the data value, where the receiver breaks the line and the tail of
	// the value becomes a forged field.
	if m.Data == "" {
		w.buf = append(w.buf, "data:\n"...)
	} else {
		for {
			nl := strings.IndexAny(m.Data, "\r\n")
			if nl < 0 {
				w.buf = appendSSEField(w.buf, "data", m.Data)
				break
			}
			w.buf = appendSSEField(w.buf, "data", m.Data[:nl])
			// \r\n 作为单个终止符消费,避免多出一个空 data 行。
			// Consume \r\n as one terminator so no extra empty data line appears.
			if m.Data[nl] == '\r' && nl+1 < len(m.Data) && m.Data[nl+1] == '\n' {
				nl++
			}
			m.Data = m.Data[nl+1:]
		}
	}
	// 事件以空行(额外一个 \n)结束。
	// An event ends with a blank line (one extra \n).
	w.buf = append(w.buf, '\n')
	return w.flush(w.buf)
}

// Comment 发送一条 SSE 注释行(以 ':' 开头),客户端会忽略其内容。空 comment 即一条纯
// 冒号心跳,是保持连接与穿透代理空闲超时的惯用手段。
// Comment emits an SSE comment line (starting with ':'), which clients ignore. An
// empty comment is a bare-colon heartbeat, the idiomatic way to keep the
// connection alive and defeat proxy idle timeouts.
func (w *SSEWriter) Comment(text string) error {
	if w.err != nil {
		return w.err
	}
	w.buf = w.buf[:0]
	w.buf = append(w.buf, ':')
	// 注释同样是单行字段:值内的换行会结束注释行,其后内容被当作真实字段解析。
	// A comment is a single-line field too: a newline inside the value ends the
	// comment line and whatever follows is parsed as a real field.
	w.buf = append(w.buf, stripSSELineBreaks(text)...)
	w.buf = append(w.buf, '\n', '\n')
	return w.flush(w.buf)
}

// Ping 发送一条空注释心跳,等价于 Comment("")。
// Ping sends an empty-comment heartbeat, equivalent to Comment("").
func (w *SSEWriter) Ping() error { return w.Comment("") }

// Err 返回首个写出错误(如客户端断开);无错为 nil。流式循环可据它退出。
// Err returns the first write error (e.g. a client disconnect); nil when none. A
// streaming loop can exit based on it.
func (w *SSEWriter) Err() error { return w.err }

// flush 写出 b 并立即冲刷;记住首个错误,后续调用短路返回它。
// flush writes b and flushes immediately; it remembers the first error and
// short-circuits to it on later calls.
func (w *SSEWriter) flush(b []byte) error {
	if _, err := w.resp.Write(b); err != nil {
		w.err = err
		return err
	}
	w.resp.Flush()
	return nil
}

// appendSSEField 追加一个 "field:value\n" 的 SSE 字段行。调用方须保证 value 不含
// \r 或 \n——SSE 以行为单位,值内的换行会被接收端当作字段边界。
// appendSSEField appends one "field:value\n" SSE field line. Callers must ensure
// value contains no \r or \n: SSE is line-oriented, so a newline inside a value is
// read as a field boundary by the receiver.
func appendSSEField(dst []byte, field, value string) []byte {
	dst = append(dst, field...)
	dst = append(dst, ':')
	dst = append(dst, value...)
	dst = append(dst, '\n')
	return dst
}

// stripSSELineBreaks 删除单行 SSE 字段值(id/event)中的 \r 与 \n。
//
// 这两个字段在协议上只能是单行,无法像 data 那样拆成多行表达。若原样写出,攻击者控制的
// ID 或 Event 值即可注入任意字段乃至整条伪造事件(如 ID = "1\ndata:INJECTED"),因此这里
// 剥离而非拒绝:SSE 无逐条报错的通道,静默丢弃非法字符比中断整个流更可用,且不改变字段语义。
// stripSSELineBreaks removes \r and \n from a single-line SSE field value (id/event).
//
// The protocol allows only one line for these fields; they cannot be split across
// lines the way data can. Emitting them verbatim would let an attacker-controlled ID
// or Event value inject arbitrary fields or whole forged events (e.g.
// ID = "1\ndata:INJECTED"). Stripping is preferred over rejecting: SSE has no
// per-message error channel, so silently dropping the illegal bytes stays usable and
// preserves the field's meaning.
func stripSSELineBreaks(s string) string {
	if strings.IndexAny(s, "\r\n") < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\r' && s[i] != '\n' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

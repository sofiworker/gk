// Package sse 实现 Server-Sent Events 的线格式编解码，供 ghttp(server)的写端与
// ghttp/client 的读端共用。
// Package sse implements Server-Sent Events wire-format encoding and decoding, shared by
// ghttp's server-side writer and ghttp/client's reader.
//
// 共享的理由与 codec 相同：线格式是同一份规范（WHATWG HTML §Server-sent events），
// 两端各写一份必然漂移——尤其在"多行 data 如何折叠""id/event 里的换行如何处置"这类
// 边界上。策略（重连、Last-Event-ID、事件类型分派）不在这里，留在各端。
// The reason to share mirrors codec: the wire format is one specification (WHATWG HTML
// §Server-sent events), and two implementations would drift — especially on boundaries
// such as how multi-line data folds and what happens to newlines inside id/event. Policy
// (reconnection, Last-Event-ID, event dispatch) stays on each side.
package sse

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// 字段名常量。两端共用同一份字面量，避免拼写漂移。
// Field-name constants. Both sides share one set of literals to prevent spelling drift.
const (
	FieldData  = "data"
	FieldEvent = "event"
	FieldID    = "id"
	FieldRetry = "retry"
)

// DefaultEventName 是未声明 event 字段时的事件名（协议规定的默认类型）。
// DefaultEventName is the event name when no event field was declared (the protocol's
// default type).
const DefaultEventName = "message"

// Event 是一个 SSE 事件。Comment 承载以 ':' 开头的注释行；它不构成事件数据，但读端常
// 用它作为心跳信号，故如实保留。
// Event is one SSE event. Comment carries a line beginning with ':'; it is not event
// data, but readers commonly use it as a heartbeat, so it is preserved verbatim.
type Event struct {
	ID      string
	Name    string
	Data    string
	Retry   time.Duration
	Comment string
}

// AppendField 追加一个 "field:value\n" 字段行。
//
// 调用方须保证 value 不含 \r 或 \n（data 的多行语义由调用方自行按行拆开），或改用
// StripLineBreaks 先行净化。SSE 以行为单位，值内的换行会被接收端当作字段边界。
// AppendField appends one "field:value\n" line.
//
// Callers must ensure value has no \r or \n (multi-line data semantics are the caller's
// job to split per line), or sanitize it with StripLineBreaks first. SSE is
// line-oriented, so a newline inside a value is read as a field boundary by the
// receiver.
func AppendField(dst []byte, field, value string) []byte {
	dst = append(dst, field...)
	dst = append(dst, ':')
	dst = append(dst, value...)
	dst = append(dst, '\n')
	return dst
}

// StripLineBreaks 删除单行 SSE 字段值（id/event）中的 \r 与 \n。
//
// 这两个字段在协议上只能是单行，无法像 data 那样拆成多行表达。若原样写出，攻击者控制的
// ID 或 Event 值即可注入任意字段乃至整条伪造事件（如 ID = "1\ndata:INJECTED"），因此这里
// 剥离而非拒绝：SSE 无逐条报错的通道，静默丢弃非法字符比中断整个流更可用，且不改变字段语义。
// StripLineBreaks removes \r and \n from a single-line SSE field value (id/event).
//
// The protocol allows only one line for these fields; they cannot be split across lines
// the way data can. Emitting them verbatim would let an attacker-controlled ID or Event
// value inject arbitrary fields or whole forged events (e.g. ID = "1\ndata:INJECTED").
// Stripping is preferred over rejecting: SSE has no per-message error channel, so
// silently dropping the illegal bytes stays usable and preserves the field's meaning.
func StripLineBreaks(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
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

// ReadEvent 从 r 读出一个事件。它读到空行（分帧边界）或流结束才返回。
//
// 返回 io.EOF 表示流已正常结束且没有剩余事件；若流在事件中途结束（最后一行无换行符），
// 已累积的字段仍会被作为一个事件返回，随后下一次调用得到 io.EOF。
// ReadEvent reads one event from r. It returns only at a blank-line boundary or when the
// stream ends.
//
// io.EOF means the stream ended cleanly with no remaining event; if the stream ends
// mid-event (the final line has no newline), the accumulated fields are still returned
// as one event and the next call yields io.EOF.
func ReadEvent(r *bufio.Reader) (Event, error) {
	var (
		ev        Event
		dataLines []string
		hasFields bool
	)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return Event{}, err
			}
			// 流结束：先把最后一行（可能没有换行符）并入，再决定是"事件"还是 EOF。
			// Stream end: fold in the final line (which may lack a newline), then decide
			// between "an event" and EOF.
			if trimmed := trimLineEnding(line); trimmed != "" {
				hasFields = parseLine(&ev, &dataLines, trimmed) || hasFields
			}
			if !hasFields {
				return Event{}, io.EOF
			}
			ev.Data = strings.Join(dataLines, "\n")
			if ev.Name == "" {
				ev.Name = DefaultEventName
			}
			return ev, nil
		}
		trimmed := trimLineEnding(line)
		if trimmed == "" {
			if !hasFields {
				// 连续空行不构成事件；继续找下一个字段。
				// Consecutive blank lines denote no event; keep looking for fields.
				continue
			}
			ev.Data = strings.Join(dataLines, "\n")
			if ev.Name == "" {
				ev.Name = DefaultEventName
			}
			return ev, nil
		}
		hasFields = parseLine(&ev, &dataLines, trimmed) || hasFields
	}
}

// parseLine 解析一行 "field: value"，返回该行是否是一个字段（注释行不是）。
//
// 注释行不构成事件（协议如此）：把它排除在 hasFields 之外，纯心跳流才不会每收到一次注释
// 就产生一个空事件。需要感知心跳的调用方应改用下一次读取的超时，而不是依赖空事件。
// parseLine parses one "field: value" line and reports whether it was a field (a comment
// line is not).
//
// Comment lines do not constitute an event (per the protocol): excluding them from
// hasFields keeps a pure heartbeat stream from emitting an empty event per comment.
// Callers wanting heartbeat visibility should bound the next read instead of relying on
// empty events.
func parseLine(ev *Event, dataLines *[]string, line string) bool {
	if strings.HasPrefix(line, ":") {
		ev.Comment = line[1:]
		return false
	}
	field, value, found := strings.Cut(line, ":")
	if !found {
		// 没有冒号的行整体作为字段名，值为空（协议允许）。
		// A line without a colon is entirely a field name with an empty value (the
		// protocol allows it).
		value = ""
	} else {
		// 冒号后只吃掉【一个】前导空格，其余空格属于值。
		// Only ONE leading space after the colon is consumed; the rest belongs to the
		// value.
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case FieldData:
		*dataLines = append(*dataLines, value)
	case FieldEvent:
		ev.Name = value
	case FieldID:
		ev.ID = value
	case FieldRetry:
		if ms, err := strconv.Atoi(value); err == nil && ms >= 0 {
			ev.Retry = time.Duration(ms) * time.Millisecond
		}
	}
	return true
}

// trimLineEnding 去掉行尾的 \n 与随附的 \r（兼容 CRLF 服务端）。
// trimLineEnding removes a trailing \n and any accompanying \r (CRLF-serving peers).
func trimLineEnding(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}

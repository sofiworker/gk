package wire

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// SSE 字段名常量（WHATWG HTML §Server-sent events）。
// SSE field-name constants (WHATWG HTML §Server-sent events).
const (
	SSEFieldData  = "data"
	SSEFieldEvent = "event"
	SSEFieldID    = "id"
	SSEFieldRetry = "retry"
)

// DefaultSSEEventName 是未声明 event 字段时的事件名（协议规定的默认类型）。
// DefaultSSEEventName is the event name when no event field was declared (the
// protocol's default type).
const DefaultSSEEventName = "message"

// SSEEvent 是一个 SSE 事件。Comment 保留最后一条以 ':' 开头的注释行内容（常用作心跳），
// 它不构成事件数据。
// SSEEvent is one SSE event. Comment keeps the last line beginning with ':' (commonly a
// heartbeat); it is not event data.
type SSEEvent struct {
	ID      string
	Name    string
	Data    string
	Retry   time.Duration
	Comment string
}

// AppendSSEField 追加一行 "field:value\n"。
//
// 调用方须保证 value 不含 \r 或 \n：多行 data 应按行拆成多次调用，id/event 应先经
// StripSSELineBreaks 净化。SSE 以行为单位，值内换行会被接收端当作字段边界。
// AppendSSEField appends one "field:value\n" line.
//
// Callers must ensure value has no \r or \n: split multi-line data into one call per
// line, and sanitize id/event with StripSSELineBreaks first. SSE is line-oriented, so a
// newline inside a value is read as a field boundary.
func AppendSSEField(dst []byte, field, value string) []byte {
	dst = append(dst, field...)
	dst = append(dst, ':')
	dst = append(dst, value...)
	return append(dst, '\n')
}

// StripSSELineBreaks 删除单行字段值（id/event）中的 \r 与 \n。
//
// 原样写出会让攻击者控制的值注入任意字段乃至伪造整条事件（如 "1\ndata:INJECTED"）。
// 选择剥离而非拒绝：SSE 没有逐条报错的通道，静默丢弃非法字节比中断整个流更可用。
// StripSSELineBreaks removes \r and \n from a single-line field value (id/event).
//
// Emitting such values verbatim lets an attacker inject fields or forge whole events
// (e.g. "1\ndata:INJECTED"). Stripping is preferred over rejecting: SSE has no
// per-message error channel, so dropping the illegal bytes beats aborting the stream.
func StripSSELineBreaks(s string) string {
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

// ReadSSEEvent 从 r 读出一个事件，读到空行（分帧边界）或流结束才返回。
//
// io.EOF 表示流已正常结束且没有剩余事件；若流在事件中途结束（末行无换行符），已累积的
// 字段仍作为一个事件返回，下一次调用再得到 io.EOF。纯注释（心跳）不产生事件。
// ReadSSEEvent reads one event from r, returning only at a blank-line boundary or at
// the end of the stream.
//
// io.EOF means the stream ended cleanly with nothing pending; if it ends mid-event (no
// final newline), the accumulated fields are still returned as one event and the next
// call yields io.EOF. Comment-only input (heartbeats) produces no event.
func ReadSSEEvent(r *bufio.Reader) (SSEEvent, error) {
	var (
		ev        SSEEvent
		dataLines []string
		hasFields bool
	)
	for {
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return SSEEvent{}, err
		}
		atEOF := err == io.EOF
		trimmed := trimLineEnding(line)
		if trimmed != "" {
			hasFields = parseSSELine(&ev, &dataLines, trimmed) || hasFields
		}
		if atEOF || trimmed == "" {
			if !hasFields {
				if atEOF {
					return SSEEvent{}, io.EOF
				}
				// 连续空行不构成事件。
				// Consecutive blank lines denote no event.
				continue
			}
			ev.Data = strings.Join(dataLines, "\n")
			if ev.Name == "" {
				ev.Name = DefaultSSEEventName
			}
			return ev, nil
		}
	}
}

// parseSSELine 解析一行 "field: value"，返回该行是否为字段（注释行不是）。
// parseSSELine parses one "field: value" line and reports whether it was a field
// (comment lines are not).
func parseSSELine(ev *SSEEvent, dataLines *[]string, line string) bool {
	if strings.HasPrefix(line, ":") {
		ev.Comment = line[1:]
		return false
	}
	field, value, found := strings.Cut(line, ":")
	if found {
		// 冒号后只吃掉一个前导空格，其余空格属于值。
		// Only one leading space after the colon is consumed; the rest is the value.
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case SSEFieldData:
		*dataLines = append(*dataLines, value)
	case SSEFieldEvent:
		ev.Name = value
	case SSEFieldID:
		// 协议规定含 NUL 的 id 必须忽略。
		// The protocol requires ignoring ids containing NUL.
		if !strings.ContainsRune(value, 0) {
			ev.ID = value
		}
	case SSEFieldRetry:
		if ms, err := strconv.Atoi(value); err == nil && ms >= 0 {
			ev.Retry = time.Duration(ms) * time.Millisecond
		}
	}
	return true
}

// trimLineEnding 去掉行尾的 \n 与随附的 \r（兼容 CRLF）。
// trimLineEnding removes a trailing \n and any accompanying \r (CRLF peers).
func trimLineEnding(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}

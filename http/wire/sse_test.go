package wire

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// TestAppendSSEField 验证输出格式。
// TestAppendSSEField verifies the output format.
func TestAppendSSEField(t *testing.T) {
	tests := []struct {
		name         string
		dst          []byte
		field, value string
		want         string
	}{
		{"basic", nil, "data", "hello", "data:hello\n"},
		{"empty value", nil, "data", "", "data:\n"},
		{"appends to existing", []byte("id:1\n"), "event", "x", "id:1\nevent:x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(AppendSSEField(tt.dst, tt.field, tt.value)); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestStripSSELineBreaks 验证换行剥离。
// TestStripSSELineBreaks verifies line-break stripping.
func TestStripSSELineBreaks(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"no breaks unchanged", "abc", "abc"},
		{"empty", "", ""},
		{"lf", "1\ndata:INJECTED", "1data:INJECTED"},
		{"crlf", "a\r\nb", "ab"},
		{"cr only", "a\rb", "ab"},
		{"only breaks", "\r\n\r\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripSSELineBreaks(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestReadSSEEvent 验证单事件解析的各种形态（一个输入只读第一个事件）。
// TestReadSSEEvent verifies single-event parsing shapes (only the first event is read).
func TestReadSSEEvent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want SSEEvent
	}{
		{"default name", "data: hi\n\n", SSEEvent{Name: "message", Data: "hi"}},
		{"multi-line data", "data: a\ndata: b\ndata: c\n\n", SSEEvent{Name: "message", Data: "a\nb\nc"}},
		{"event id retry", "event: tick\nid: 7\nretry: 1500\ndata: x\n\n",
			SSEEvent{Name: "tick", ID: "7", Retry: 1500 * time.Millisecond, Data: "x"}},
		{"invalid retry ignored", "retry: abc\ndata: x\n\n", SSEEvent{Name: "message", Data: "x"}},
		{"negative retry ignored", "retry: -5\ndata: x\n\n", SSEEvent{Name: "message", Data: "x"}},
		{"only one space stripped", "data:  two\n\n", SSEEvent{Name: "message", Data: " two"}},
		{"no space after colon", "data:x\n\n", SSEEvent{Name: "message", Data: "x"}},
		{"line without colon", "data\n\n", SSEEvent{Name: "message", Data: ""}},
		{"crlf", "event: e\r\ndata: d\r\n\r\n", SSEEvent{Name: "e", Data: "d"}},
		{"comment plus data", ": hb\ndata: x\n\n", SSEEvent{Name: "message", Data: "x", Comment: " hb"}},
		{"last comment wins", ":a\n:b\ndata: x\n\n", SSEEvent{Name: "message", Data: "x", Comment: "b"}},
		{"leading blank lines skipped", "\n\n\ndata: x\n\n", SSEEvent{Name: "message", Data: "x"}},
		{"id with NUL ignored", "id: a\x00b\ndata: x\n\n", SSEEvent{Name: "message", Data: "x"}},
		{"unknown field is still a field", "foo: bar\n\n", SSEEvent{Name: "message"}},
		{"mid-stream end without newline", "data: x", SSEEvent{Name: "message", Data: "x"}},
		{"mid-stream end after newline, no blank", "data: x\n", SSEEvent{Name: "message", Data: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := ReadSSEEvent(bufio.NewReader(strings.NewReader(tt.in)))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev != tt.want {
				t.Fatalf("got %+v, want %+v", ev, tt.want)
			}
		})
	}
}

// TestReadSSEEventEOF 验证无事件时返回 io.EOF。
// TestReadSSEEventEOF verifies io.EOF when no event exists.
func TestReadSSEEventEOF(t *testing.T) {
	tests := []struct{ name, in string }{
		{"empty stream", ""},
		{"comment only", ": heartbeat\n\n: again\n"},
		{"comment only no newline", ":hb"},
		{"blank lines only", "\n\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadSSEEvent(bufio.NewReader(strings.NewReader(tt.in)))
			if err != io.EOF {
				t.Fatalf("err = %v, want io.EOF", err)
			}
		})
	}
}

// TestReadSSEEventSequence 验证连续读取：事件后紧接 EOF。
// TestReadSSEEventSequence verifies successive reads: EOF right after the last event.
func TestReadSSEEventSequence(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("data: 1\n\n: hb\n\ndata: 2"))
	for _, want := range []string{"1", "2"} {
		ev, err := ReadSSEEvent(br)
		if err != nil || ev.Data != want {
			t.Fatalf("got (%+v, %v), want data %q", ev, err, want)
		}
	}
	if _, err := ReadSSEEvent(br); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

// errReader 先返回 data 再返回 err。
// errReader returns data first, then err.
type errReader struct {
	data string
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// TestReadSSEEventReaderError 非 EOF 错误应透传。
// TestReadSSEEventReaderError: non-EOF errors are passed through.
func TestReadSSEEventReaderError(t *testing.T) {
	sentinel := errors.New("boom")
	for _, data := range []string{"", "data: x\n"} {
		_, err := ReadSSEEvent(bufio.NewReader(&errReader{data: data, err: sentinel}))
		if !errors.Is(err, sentinel) {
			t.Fatalf("data %q: err = %v, want sentinel", data, err)
		}
	}
}

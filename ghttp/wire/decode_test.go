package wire

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type jsonT struct {
	A int    `json:"a"`
	B string `json:"b"`
}

type xmlT struct {
	XMLName xml.Name `xml:"item"`
	A       int      `xml:"a"`
}

// wrapEOFReader 返回被包装的 EOF。
// wrapEOFReader returns a wrapped EOF.
type wrapEOFReader struct{}

func (wrapEOFReader) Read([]byte) (int, error) { return 0, fmt.Errorf("x: %w", io.EOF) }

// maxBytesBody 用 http.MaxBytesReader 包装 s，限额 limit。
// maxBytesBody wraps s with http.MaxBytesReader at limit.
func maxBytesBody(s string, limit int64) io.Reader {
	return http.MaxBytesReader(nil, io.NopCloser(strings.NewReader(s)), limit)
}

func isMaxBytesErr(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// TestDecodeJSON 表驱动覆盖成功与格式错误路径。
// TestDecodeJSON is a table covering success and format-error paths.
func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		nilBody bool
		length  int64
		opts    []DecodeOption
		want    jsonT
		invalid bool
	}{
		{name: "ok", body: `{"a":1,"b":"x"}`, length: 15, want: jsonT{1, "x"}},
		{name: "ok unknown length", body: `{"a":2}`, length: -1, want: jsonT{A: 2}},
		{name: "empty length 0", body: ``, length: 0},
		{name: "empty length -1", body: ``, length: -1},
		{name: "nil reader", nilBody: true, length: 0},
		{name: "nil reader require body", nilBody: true, length: 0, opts: []DecodeOption{RequireBody()}, invalid: true},
		{name: "nil reader positive length", nilBody: true, length: 5, invalid: true},
		{name: "truncated: positive length empty body", body: ``, length: 10, invalid: true},
		{name: "require body empty", body: ``, length: 0, opts: []DecodeOption{RequireBody()}, invalid: true},
		{name: "require body empty unknown length", body: ``, length: -1, opts: []DecodeOption{RequireBody()}, invalid: true},
		{name: "require body with body ok", body: `{"a":1}`, length: -1, opts: []DecodeOption{RequireBody()}, want: jsonT{A: 1}},
		{name: "unknown field default ok", body: `{"a":1,"z":2}`, length: -1, want: jsonT{A: 1}},
		{name: "unknown field disallowed", body: `{"a":1,"z":2}`, length: -1, opts: []DecodeOption{DisallowUnknownFields()}, invalid: true},
		{name: "nil option no panic", body: `{"a":1}`, length: -1, opts: []DecodeOption{nil, RequireBody(), nil}, want: jsonT{A: 1}},
		{name: "trailing second value", body: `{"a":1}{"a":2}`, length: -1, invalid: true},
		{name: "trailing garbage", body: `{"a":1} x`, length: -1, invalid: true},
		{name: "trailing bracket", body: `{"a":1}]`, length: -1, invalid: true},
		{name: "trailing brace", body: `{"a":1}}`, length: -1, invalid: true},
		{name: "trailing whitespace ok", body: "{\"a\":1}  \t\r\n\n", length: -1, want: jsonT{A: 1}},
		{name: "truncated value", body: `{"a":`, length: -1, invalid: true},
		{name: "bad syntax", body: `{a:1}`, length: -1, invalid: true},
		{name: "type mismatch", body: `{"a":"str"}`, length: -1, invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r io.Reader
			if !tt.nilBody {
				r = strings.NewReader(tt.body)
			}
			var got jsonT
			err := DecodeJSON(r, &got, tt.length, tt.opts...)
			if tt.invalid {
				if !errors.Is(err, ErrInvalidFormat) {
					t.Fatalf("err = %v, want ErrInvalidFormat", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestDecodeJSONSyntaxErrorChain 语法错误同时可 errors.As 与 errors.Is。
// TestDecodeJSONSyntaxErrorChain: syntax errors satisfy both errors.As and errors.Is.
func TestDecodeJSONSyntaxErrorChain(t *testing.T) {
	var v jsonT
	err := DecodeJSON(strings.NewReader(`{"a":}`), &v, -1)
	var se *json.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("want *json.SyntaxError in chain, got %v", err)
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("want ErrInvalidFormat, got %v", err)
	}
}

// TestDecodeWrappedEOF 包装过的 EOF 视为空体。
// TestDecodeWrappedEOF: a wrapped EOF is treated as an empty body.
func TestDecodeWrappedEOF(t *testing.T) {
	v := jsonT{}
	if err := DecodeJSON(wrapEOFReader{}, &v, -1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != (jsonT{}) {
		t.Fatalf("target not zero: %+v", v)
	}
	if err := DecodeJSON(wrapEOFReader{}, &v, 3); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("positive length: err = %v, want ErrInvalidFormat", err)
	}
	if err := DecodeXML(wrapEOFReader{}, &xmlT{}, -1); err != nil {
		t.Fatalf("xml: unexpected error: %v", err)
	}
}

// TestDecodeJSONMaxBytes 超限不得归为格式错误。
// TestDecodeJSONMaxBytes: size overrun must not be classified as a format error.
func TestDecodeJSONMaxBytes(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		limit int64
	}{
		{"overrun inside first value", `{"a":1,"b":"` + strings.Repeat("x", 100) + `"}`, 20},
		{"valid value, overrun in tail", `{"a":1}` + strings.Repeat(" ", 100), 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v jsonT
			err := DecodeJSON(maxBytesBody(tt.body, tt.limit), &v, -1)
			if !isMaxBytesErr(err) {
				t.Fatalf("want *http.MaxBytesError in chain, got %v", err)
			}
			if errors.Is(err, ErrInvalidFormat) {
				t.Fatalf("MaxBytesError must not be ErrInvalidFormat: %v", err)
			}
		})
	}
}

// TestDecodeXML 表驱动覆盖 XML 路径。
// TestDecodeXML is a table covering the XML paths.
func TestDecodeXML(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		nilBody bool
		length  int64
		opts    []DecodeOption
		want    int
		invalid bool
	}{
		{name: "ok", body: `<item><a>3</a></item>`, length: -1, want: 3},
		{name: "empty", body: ``, length: 0},
		{name: "empty unknown length", body: ``, length: -1},
		{name: "nil reader", nilBody: true, length: 0},
		{name: "nil reader require body", nilBody: true, length: -1, opts: []DecodeOption{RequireBody()}, invalid: true},
		{name: "truncated: positive length empty", body: ``, length: 9, invalid: true},
		{name: "require body empty", body: ``, length: 0, opts: []DecodeOption{RequireBody(), nil}, invalid: true},
		{name: "trailing whitespace ok", body: "<item><a>1</a></item>\n  \n", length: -1, want: 1},
		{name: "second element", body: `<item><a>1</a></item><item><a>2</a></item>`, length: -1, invalid: true},
		{name: "trailing text", body: `<item><a>1</a></item> junk`, length: -1, invalid: true},
		{name: "trailing syntax error", body: `<item><a>1</a></item></x>`, length: -1, invalid: true},
		{name: "syntax error", body: `<item><a>1</item>`, length: -1, invalid: true},
		{name: "unclosed element", body: `<item><a>1</a>`, length: -1, invalid: true},
		{name: "disallow unknown has no effect", body: `<item><a>1</a><z/></item>`, length: -1, opts: []DecodeOption{DisallowUnknownFields()}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r io.Reader
			if !tt.nilBody {
				r = strings.NewReader(tt.body)
			}
			var got xmlT
			err := DecodeXML(r, &got, tt.length, tt.opts...)
			if tt.invalid {
				if !errors.Is(err, ErrInvalidFormat) {
					t.Fatalf("err = %v, want ErrInvalidFormat", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.A != tt.want {
				t.Fatalf("A = %d, want %d", got.A, tt.want)
			}
		})
	}
}

// TestDecodeXMLSyntaxErrorChain 语法错误可 errors.As 出 *xml.SyntaxError。
// TestDecodeXMLSyntaxErrorChain: syntax errors expose *xml.SyntaxError.
func TestDecodeXMLSyntaxErrorChain(t *testing.T) {
	err := DecodeXML(strings.NewReader(`<item><a>1</item>`), &xmlT{}, -1)
	var se *xml.SyntaxError
	if !errors.As(err, &se) || !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("err = %v, want *xml.SyntaxError wrapped by ErrInvalidFormat", err)
	}
}

// TestDecodeXMLMaxBytes 超限不得归为格式错误。
// TestDecodeXMLMaxBytes: size overrun must not be classified as a format error.
func TestDecodeXMLMaxBytes(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		limit int64
	}{
		{"overrun inside first element", `<item><a>1</a><b>` + strings.Repeat("x", 100) + `</b></item>`, 20},
		{"valid element, overrun in tail", `<item><a>1</a></item>` + strings.Repeat(" ", 100), 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DecodeXML(maxBytesBody(tt.body, tt.limit), &xmlT{}, -1)
			if !isMaxBytesErr(err) {
				t.Fatalf("want *http.MaxBytesError in chain, got %v", err)
			}
			if errors.Is(err, ErrInvalidFormat) {
				t.Fatalf("MaxBytesError must not be ErrInvalidFormat: %v", err)
			}
		})
	}
}

// BenchmarkDecodeJSON 小对象解码基准。
// BenchmarkDecodeJSON benchmarks decoding a small object.
func BenchmarkDecodeJSON(b *testing.B) {
	const body = `{"a":1,"b":"hello"}`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v jsonT
		if err := DecodeJSON(strings.NewReader(body), &v, int64(len(body)), DisallowUnknownFields()); err != nil {
			b.Fatal(err)
		}
	}
}

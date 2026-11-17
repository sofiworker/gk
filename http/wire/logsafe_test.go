package wire

import "testing"

// TestLogToken 覆盖转义规则。
// TestLogToken covers the escaping rules.
func TestLogToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "/api/v1/users?id=1", "/api/v1/users?id=1"},
		{"utf8 untouched", "你好 world", "你好 world"},
		{"newline", "a\nb", `a\nb`},
		{"carriage return", "a\rb", `a\rb`},
		{"tab", "a\tb", `a\tb`},
		{"crlf injection", "x\r\nINFO forged", `x\r\nINFO forged`},
		{"nul two hex digits", "a\x00b", `a\x00b`},
		{"control followed by hex-like char", "\x01f", `\x01f`},
		{"escape 0x1b", "\x1b[31m", `\x1b[31m`},
		{"del", "a\x7fb", `a\x7fb`},
		{"0x1f boundary", "\x1f", `\x1f`},
		{"space not escaped", " ", " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LogToken(tt.in); got != tt.want {
				t.Fatalf("LogToken(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestLogTokenNoAlloc 无控制字符时不应分配。
// TestLogTokenNoAlloc: no allocation when there are no control characters.
func TestLogTokenNoAlloc(t *testing.T) {
	s := "/plain/path?x=1"
	var sink string
	if n := testing.AllocsPerRun(100, func() { sink = LogToken(s) }); n != 0 {
		t.Fatalf("allocs = %v, want 0", n)
	}
	_ = sink
}

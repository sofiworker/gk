package wire

import "strings"

// LogToken 转义控制字符与 DEL，使结果可安全写入单行日志字段（路径、查询串、错误文本、
// URL、panic 值等）。
//
// 它只消除折行能力，不做脱敏：凭证（Authorization、token）的处理由各调用方按自己的字段
// 清单负责。日志多按行采集，一个未转义的换行就能凭空伪造一条完整记录。
// LogToken escapes control characters and DEL so the result is safe inside a
// single-line log field (path, query, error text, URL, panic value, …).
//
// It only removes the ability to break lines and performs no redaction: credentials
// (Authorization, tokens) are each caller's job against its own field list. Logs are
// mostly collected line by line, so one unescaped newline can forge a whole record.
func LogToken(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	const hex = "0123456789abcdef"
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20 || c == 0x7f:
			// 固定两位十六进制，避免 "\x0" 与后续字符粘连成歧义序列。
			// Always two hex digits so "\x0" never merges with the next character.
			b.WriteString(`\x`)
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

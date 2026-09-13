// Package logsafe 提供日志字段的单行化净化，供 server 的访问日志与 client 的 debug
// dump 共用。
// Package logsafe provides single-line sanitization of log fields, shared by the
// server's access log and the client's debug dump.
//
// 它只做"消除折行能力"，不做脱敏：不改语义、不删信息。凭证脱敏（Authorization、
// token）由各调用方按自己的字段清单处理，因为两端的敏感字段集合不同。
// It only removes the ability to break a line; it performs no redaction — it neither
// changes meaning nor removes information. Credential redaction (Authorization,
// tokens) stays with each caller against its own field list, since the two sides
// have different sensitive-field sets.
package logsafe

import "strings"

// Token 转义字符串中的控制字符与 DEL，使其可安全写进单行日志字段（路径、错误文本、
// panic 值、URL、响应体片段等）。
//
// 为何必要：路径里的控制字符已在路由校验层被拒（400），但【查询串】与由它派生的错误
// 文案不受该限制——`?name=x%0aFAKE` 会原样出现在 err.Error() 里。日志若按行解析（绝大多数
// 采集器如此），一个换行就能凭空造出一条完整记录，足以伪造"管理员登录成功"之类的事件，
// 且事后无法与真实记录区分。
// Token escapes control characters and DEL so the result is safe in a single-line
// log field (path, error text, panic value, URL, response-body fragment, …).
//
// Why it is needed: control characters in the PATH are already refused (400) at
// route validation, but the QUERY string and error text derived from it are not — a
// value like `?name=x%0aFAKE` appears verbatim inside err.Error(). Logs are parsed
// line by line by most collectors, so a single newline lets a client mint a whole
// extra record, enough to forge an event such as "admin login succeeded" with no way
// to tell it from real ones afterwards.
func Token(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 || c == 0x7f {
				// 固定两位：`\x` + 零填充十六进制，避免 \x0 与后续字面字符粘连成
				// 歧义序列（"a\x00b" 曾被写成 "a\x0b"，读起来像 \x0b）。
				// Two fixed digits: `\x` plus zero-padded hex, so \x0 can never run
				// into the following literal and read ambiguously ("a\x00b" was once
				// written as "a\x0b", which looks like \x0b).
				b.WriteString(`\x`)
				const hex = "0123456789abcdef"
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

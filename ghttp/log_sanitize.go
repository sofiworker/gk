package ghttp

import "github.com/sofiworker/gk/ghttp/internal/logsafe"

// sanitizeLogToken 把日志字段净化为单行形式；实现下沉到 ghttp/internal/logsafe，
// 与 client 的 debug dump 共用同一套转义规则（控制字符与 DEL 转义，见该包注释）。
// sanitizeLogToken normalizes a log field to a single line; the implementation lives
// in ghttp/internal/logsafe so the client's debug dump shares one escaping rule
// (control characters and DEL), documented in that package.
func sanitizeLogToken(s string) string { return logsafe.Token(s) }

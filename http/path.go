package ghttp

import (
	"path"
	"strings"
)

// normalizeRoutePath 把模板写法转换为 Gin 写法：{id} → :id，{path...} → *path。
// 不含花括号的路径原样返回。
// normalizeRoutePath converts template syntax to Gin syntax: {id} → :id and
// {path...} → *path. Paths without braces are returned unchanged.
func normalizeRoutePath(p string) (string, error) {
	if !strings.ContainsAny(p, "{}") {
		return p, nil
	}
	var b strings.Builder
	b.Grow(len(p))
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '}' {
			return "", errInvalidTemplate(p)
		}
		if c != '{' {
			b.WriteByte(c)
			continue
		}
		end := strings.IndexByte(p[i:], '}')
		if end < 0 {
			return "", errInvalidTemplate(p)
		}
		name := p[i+1 : i+end]
		// 模板参数必须独占一个路径段
		// A template parameter must occupy a whole path segment
		if p[i-1] != '/' || (i+end+1 < len(p) && p[i+end+1] != '/') {
			return "", errInvalidTemplate(p)
		}
		prefix := byte(':')
		if base, ok := strings.CutSuffix(name, "..."); ok {
			name, prefix = base, '*'
		}
		if name == "" || strings.ContainsAny(name, "{}:*/") {
			return "", errInvalidTemplate(p)
		}
		b.WriteByte(prefix)
		b.WriteString(name)
		i += end
	}
	return b.String(), nil
}

// errInvalidTemplate 返回模板写法错误，包装 ErrInvalidRoutePath。
// errInvalidTemplate returns a template syntax error wrapping ErrInvalidRoutePath.
func errInvalidTemplate(p string) error {
	return &routePathError{path: p, reason: "invalid {param} template"}
}

// routePathError 描述非法的路由路径，errors.Is(err, ErrInvalidRoutePath) 成立。
// routePathError describes an invalid route path; errors.Is(err, ErrInvalidRoutePath) holds.
type routePathError struct {
	path   string
	reason string
}

// Error 实现 error。
// Error implements error.
func (e *routePathError) Error() string {
	return ErrInvalidRoutePath.Error() + ": " + e.reason + ": " + e.path
}

// Unwrap 返回 ErrInvalidRoutePath。
// Unwrap returns ErrInvalidRoutePath.
func (e *routePathError) Unwrap() error { return ErrInvalidRoutePath }

// joinPaths 拼接 Group 前缀与相对路径：清理多余的 '/'，并保留相对路径的尾斜杠语义。
// joinPaths joins a group prefix and a relative path, collapsing duplicate '/' and keeping
// the relative path's trailing slash.
func joinPaths(prefix, rel string) string {
	if rel == "" {
		return prefix
	}
	if prefix == "" {
		return rel
	}
	joined := path.Join(prefix, rel)
	if strings.HasSuffix(rel, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}

// validRequestPath 校验请求路径：必须以 '/' 开头，不得含 "." 或 ".." 段与控制字符；
// strict 时额外拒绝空段（"//"）。
//
// 实现为单遍紧循环 + 定长 needle：循环内同时检查控制字符并记录是否出现 '.'
// （无 '.' 则不可能有 "."/".." 段）；点段必然包含子串 "/."，以其作门控后再用
// "/./"、"/../"、后缀 "/."、"/.." 四个定长匹配判定；内部空段与子串 "//" 一一对应
// （尾部单个 '/' 的空段允许保留）。等价性由 TestValidRequestPathEquivalence 穷举验证。
//
// validRequestPath validates a request path: it must start with '/', contain no "." or
// ".." segments and no control characters; strict additionally rejects empty segments
// ("//").
//
// It is one tight loop plus fixed needles: the loop checks control characters and notes
// whether '.' appears (a path without '.' cannot contain "."/".." segments); every dot
// segment contains the substring "/.", which gates the fixed needles "/./", "/../" and
// the suffixes "/.", "/.."; an interior empty segment corresponds exactly to the
// substring "//" (a single trailing '/' stays allowed). Equivalence with the original
// implementation is exhaustively verified by TestValidRequestPathEquivalence.
func validRequestPath(p string, strict bool) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	hasDot := false
	for i := 1; i < len(p); i++ {
		c := p[i]
		if c < 0x20 || c == 0x7f {
			return false
		}
		if c == '.' {
			hasDot = true
		}
	}
	if hasDot && strings.Contains(p, "/.") &&
		(strings.Contains(p, "/./") || strings.Contains(p, "/../") ||
			strings.HasSuffix(p, "/.") || strings.HasSuffix(p, "/..")) {
		return false
	}
	if strict && strings.Contains(p, "//") {
		return false
	}
	return true
}

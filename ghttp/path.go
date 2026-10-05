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
// validRequestPath validates a request path: it must start with '/', contain no "." or
// ".." segments and no control characters; strict additionally rejects empty segments
// ("//").
func validRequestPath(p string, strict bool) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	start := 1
	for i := 1; i <= len(p); i++ {
		if i < len(p) {
			c := p[i]
			if c < 0x20 || c == 0x7f {
				return false
			}
			if c != '/' {
				continue
			}
		}
		seg := p[start:i]
		switch {
		case seg == "." || seg == "..":
			return false
		case seg == "" && strict && i < len(p):
			return false
		}
		start = i + 1
	}
	return true
}

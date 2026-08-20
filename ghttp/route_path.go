package ghttp

import (
	"fmt"
	"strings"
)

// 本文件负责路径处理:注册期把本框架的模板语法翻译为 gin 形式并校验;请求期对
// 请求路径做零分配的合法性预检。
// This file handles paths: at registration time it translates this framework's
// template syntax into gin form with validation; at request time it pre-checks
// the request path with zero allocation.
//
// 模板语法 / template syntax:
//   {name}     单段参数     → gin :name   / single-segment param
//   {name...}  尾部通配      → gin *name   / trailing catch-all
// 翻译为 gin 形式后交给 route_tree.go 的 addRoute(算法与语义全对齐 gin)。
// Translated to gin form, then handed to route_tree.go's addRoute (algorithm and
// semantics fully aligned with gin).

// translateTemplate 把注册模板翻译为 gin 形式路径(:name / *name)并做注册期校验。
// 返回可直接喂给 addRoute 的路径串。
// translateTemplate translates a registration template into a gin-form path
// (:name / *name) with registration-time validation. It returns a path string
// ready for addRoute.
func translateTemplate(path string) (string, error) {
	if path == "" || path[0] != '/' {
		return "", ErrEmptyPath
	}
	if path == "/" {
		return "/", nil
	}

	var b strings.Builder
	b.Grow(len(path))

	i := 0
	for i < len(path) {
		c := path[i]
		if c != '{' {
			// gin 形式路径中 ':' 和 '*' 是保留字,模板字面量里不允许出现,避免歧义。
			// ':' and '*' are reserved in gin-form paths; disallow them in
			// template literals to avoid ambiguity.
			if c == ':' || c == '*' {
				return "", fmt.Errorf("%w: reserved character %q in literal", ErrInvalidParam, string(c))
			}
			b.WriteByte(c)
			i++
			continue
		}

		// 遇到 "{":必须紧跟在 "/" 之后(参数独占一段的起点)。
		// A "{" must immediately follow "/" (a param starts its own segment).
		if i == 0 || path[i-1] != '/' {
			return "", fmt.Errorf("%w: param must follow '/' in %q", ErrInvalidParam, path)
		}
		close := strings.IndexByte(path[i:], '}')
		if close < 0 {
			return "", fmt.Errorf("%w: unterminated '{' in %q", ErrInvalidParam, path)
		}
		inner := path[i+1 : i+close]
		next := i + close + 1

		if strings.HasSuffix(inner, "...") { // catch-all
			name := inner[:len(inner)-3]
			if err := validateParamName(name); err != nil {
				return "", err
			}
			if next != len(path) {
				return "", fmt.Errorf("%w: %q", ErrCatchAllPosition, path)
			}
			// gin catch-all 形式:*name(前导 "/" 已在 literal 中写入)。
			// gin catch-all form: *name (the leading "/" was already written).
			b.WriteByte('*')
			b.WriteString(name)
		} else { // param
			if err := validateParamName(inner); err != nil {
				return "", err
			}
			b.WriteByte(':')
			b.WriteString(inner)
		}

		// "{" 后必须紧跟 "/" 或路径结束(参数独占整段)。
		// After "}" the next byte must be "/" or end of path (param owns the segment).
		if next < len(path) && path[next] != '/' {
			return "", fmt.Errorf("%w: param must span a whole segment in %q", ErrInvalidParam, path)
		}
		i = next
	}
	return b.String(), nil
}

// validateParamName 校验参数名合法(非空、无保留字符)。
// validateParamName validates a parameter name (non-empty, no reserved chars).
func validateParamName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty name", ErrInvalidParam)
	}
	if strings.ContainsAny(name, "/{}:*") {
		return fmt.Errorf("%w: %q contains reserved character", ErrInvalidParam, name)
	}
	return nil
}

// validateRequestPath 校验请求路径;不改写路径(零分配)。尾斜杠不在此处理——由匹配
// 层的 TSR 机制(与 gin 一致)建议重定向。根路径 "/" 直接放行。
//
// strict=false(默认快速模式):仅拦 dot 段(防路径遍历)。先用一次 SIMD IndexByte
// 粗筛 '.';无 '.' 则不可能有 dot 段,立即放行(REST 路径常态)。空段(如 //)放行,
// 交给匹配层(与 gin 一致)。
// strict=true(严格模式):完整逐段校验,dot 段与空段任一非法即返回错误。
// validateRequestPath validates the request path without rewriting it (zero
// allocation). The trailing slash is left to the match layer's TSR (as in gin).
// The root "/" passes.
//
// strict=false (default fast mode): reject only dot segments (path-traversal
// guard). A single SIMD IndexByte pre-screens for '.'; with no '.' a dot segment
// is impossible and the path passes immediately (the REST norm). Empty segments
// (e.g. //) pass through to matching (aligned with gin).
// strict=true (strict mode): full per-segment validation, rejecting any dot or
// empty segment.
func validateRequestPath(path string, strict bool) error {
	if path == "" || path[0] != '/' {
		return fmt.Errorf("%w: %q must start with '/'", ErrInvalidRequestPath, path)
	}
	if path == "/" {
		return nil
	}

	if !strict {
		// 快速模式:无 '.' 直接放行;有 '.' 才细查是否存在 dot 段。
		// Fast mode: no '.' → pass; only scan for a dot segment when '.' exists.
		if strings.IndexByte(path, '.') < 0 {
			return nil
		}
		return checkDotSegments(path)
	}

	// 严格模式:逐段校验 dot 段 + 空段(零切片、零字符串比较)。
	// Strict mode: per-segment validation of dot and empty segments (zero
	// slice/compare).
	segLen := 0
	allDots := true
	for i := 1; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			if segLen == 0 {
				if i != len(path) { // 结尾单斜杠交给 TSR / trailing slash left to TSR
					return fmt.Errorf("%w: %q contains an empty segment", ErrInvalidRequestPath, path)
				}
			} else if allDots && segLen <= 2 { // "." 或 ".." / "." or ".."
				return fmt.Errorf("%w: %q contains a dot segment", ErrInvalidRequestPath, path)
			}
			segLen = 0
			allDots = true
			continue
		}
		if path[i] != '.' {
			allDots = false
		}
		segLen++
	}
	return nil
}

// checkDotSegments 仅校验 dot 段(整段为 "." 或 "..")→ 错误;不管空段。
// 关键优化:dot 段必然形如 "/." 后紧跟 "/" 或路径结束,或 "/.." 后紧跟 "/" 或结束。
// 因此只需定位每个 "/." 二元组再看其后一字节,而非逐段扫描——这样 "site.css" 这类
// 段内点号(文件扩展名)完全不触发慢逻辑。
// checkDotSegments validates only dot segments (a whole segment "." or "..") →
// error; it ignores empty segments. Key optimization: a dot segment must look
// like "/." followed by "/" or end, or "/.." followed by "/" or end. So it only
// locates each "/." pair and checks the following byte, rather than scanning per
// segment — this way an in-segment dot like "site.css" (a file extension) never
// triggers the slow logic.
func checkDotSegments(path string) error {
	for i := 0; i+1 < len(path); {
		// 定位下一个 '.'。
		// Locate the next '.'.
		j := strings.IndexByte(path[i:], '.')
		if j < 0 {
			return nil
		}
		p := i + j
		// dot 段要求 '.' 的前一字节是 '/'(段起点)。
		// A dot segment requires the byte before '.' to be '/' (segment start).
		if p == 0 || path[p-1] == '/' {
			// 段为 "." :其后是 '/' 或路径结束。
			// Segment "." : followed by '/' or end of path.
			if p+1 == len(path) || path[p+1] == '/' {
				return fmt.Errorf("%w: %q contains a dot segment", ErrInvalidRequestPath, path)
			}
			// 段为 ".." :第二个 '.' 之后是 '/' 或结束。
			// Segment ".." : after a second '.', followed by '/' or end.
			if path[p+1] == '.' && (p+2 == len(path) || path[p+2] == '/') {
				return fmt.Errorf("%w: %q contains a dot segment", ErrInvalidRequestPath, path)
			}
		}
		i = p + 1
	}
	return nil
}

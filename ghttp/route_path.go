package ghttp

import (
	"fmt"
	"go/token"
	"net/url"
	"strings"
)

type routeSegmentKind uint8

const (
	routeSegmentStatic routeSegmentKind = iota
	routeSegmentParameter
	routeSegmentCatchAll
)

type routeSegment struct {
	kind  routeSegmentKind
	value string
	// rawValue 是注册时段的原始(转义)文本,仅用于静态匹配。
	// rawValue is the raw escaped text as registered, used only for static matching.
	rawValue string
}

type routePattern struct {
	path     string
	segments []routeSegment
	trailing bool
	strict   bool
}

type requestPath struct {
	raw      string
	segments pathSegmentList
	trailing bool
}

// pathSegment 记录原始路径中一段的字节偏移。
// pathSegment records the byte offsets of one raw path segment.
type pathSegment struct {
	start int
	end   int
}

type pathSegmentList struct {
	values   [maxStackPathSegments]pathSegment
	overflow []pathSegment
	len      int
}

func (s *pathSegmentList) Add(value pathSegment) {
	if s.len < len(s.values) {
		s.values[s.len] = value
		s.len++
		return
	}
	s.overflow = append(s.overflow, value)
}

// Truncate 回退段列表到 length,撤销未成功分支的记录(零分配,保留容量)。
// Truncate rewinds the segment list to length, undoing records of failed
// match branches without allocating and while keeping capacity.
func (s *pathSegmentList) Truncate(length int) {
	if length <= s.len {
		s.len = length
		if s.overflow != nil {
			s.overflow = s.overflow[:0]
		}
		return
	}
	s.overflow = s.overflow[:length-s.len]
}

func (s pathSegmentList) Len() int {
	return s.len + len(s.overflow)
}

func (s pathSegmentList) At(index int) pathSegment {
	if index < s.len {
		return s.values[index]
	}
	return s.overflow[index-s.len]
}

// walkParamValue 是遍历期按槽位序内联收集的参数值:escaped 记录原段是否含
// '%',解码推迟到命中后的命名映射(与 fillMatchedParams 的错误语义一致)。
// walkParamValue is a param value collected inline during the walk in slot
// order: escaped records whether the raw segment contains '%', deferring the
// decode to the post-hit named mapping (matching fillMatchedParams errors).
type walkParamValue struct {
	value   string
	escaped bool
}

// paramValueList 是遍历期的位置参数值表:与 pathSegmentList 同构的栈内数组 +
// 溢出切片,回溯用 Truncate 撤销失败分支的记录。
// paramValueList is the positional param value table used while walking: a
// stack array plus overflow slice mirroring pathSegmentList, with Truncate
// unwinding failed-branch records.
// maxWalkParamValues 是遍历期位置值表的栈内槽位:与 Ctx 池化参数列表的
// maxStackPathParams 解耦——值表只活在 match 的栈帧里,可负担更大数组,让
// 常规参数路由零溢出。
// maxWalkParamValues is the inline slot count of the positional value table:
// decoupled from maxStackPathParams of the pooled param list, because the
// table only lives on match's stack frame and can afford a larger array,
// keeping ordinary parametric routes free of overflow appends.
const maxWalkParamValues = 16

type paramValueList struct {
	values   [maxWalkParamValues]walkParamValue
	overflow []walkParamValue
	len      int
}

func (s *paramValueList) Add(value walkParamValue) {
	if s.len < len(s.values) {
		s.values[s.len] = value
		s.len++
		return
	}
	s.overflow = append(s.overflow, value)
}

// Truncate 回退值表到 length,撤销未成功分支的内联记录(零分配,保留容量)。
// Truncate rewinds the value list to length, undoing inline records of failed
// branches without allocating and while keeping capacity.
func (s *paramValueList) Truncate(length int) {
	if length <= s.len {
		s.len = length
		if s.overflow != nil {
			s.overflow = s.overflow[:0]
		}
		return
	}
	s.overflow = s.overflow[:length-s.len]
}

func (s paramValueList) Len() int {
	return s.len + len(s.overflow)
}

func (s paramValueList) At(index int) walkParamValue {
	if index < s.len {
		return s.values[index]
	}
	return s.overflow[index-s.len]
}

// RawAt 返回第 index 段的原始(未解码)字节。
// RawAt returns the raw, undecoded bytes of segment index.
func (p requestPath) RawAt(index int) string {
	segment := p.segments.At(index)
	if segment.start < 0 || segment.start > segment.end || segment.end > len(p.raw) {
		return ""
	}
	return p.raw[segment.start:segment.end]
}

// DecodeAt 按需解码第 index 段。无 % 转义时零拷贝返回原始段。
// DecodeAt decodes segment index on demand. Returns the raw segment without
// copying when no percent-encoding is present.
func (p requestPath) DecodeAt(index int) (string, error) {
	raw := p.RawAt(index)
	if strings.IndexByte(raw, '%') < 0 {
		return raw, nil
	}
	return url.PathUnescape(raw)
}

// RawJoinFrom 返回自 index 起的原始段连接(保留 `/` 分隔,未解码)。
// RawJoinFrom joins raw segments from index onward, keeping '/' separators.
func (p requestPath) RawJoinFrom(index int) string {
	if index >= p.segments.Len() {
		return ""
	}
	first := p.segments.At(index)
	last := p.segments.At(p.segments.Len() - 1)
	if first.start < 0 || first.start > last.end || last.end > len(p.raw) {
		return ""
	}
	return p.raw[first.start:last.end]
}

func parseRoutePattern(rawPath string, strict bool) (routePattern, error) {
	rawSegments, trailing, err := splitEscapedPath(rawPath, ErrRoutePathInvalid)
	if err != nil {
		return routePattern{}, err
	}

	pattern := routePattern{
		segments: make([]routeSegment, 0, len(rawSegments)),
		strict:   strict,
	}
	seenParams := make(map[string]struct{}, len(rawSegments))
	for i, rawSegment := range rawSegments {
		segment, err := parseRouteSegment(rawSegment, i == len(rawSegments)-1, seenParams)
		if err != nil {
			return routePattern{}, fmt.Errorf("%w: %v", ErrRoutePathInvalid, err)
		}
		pattern.segments = append(pattern.segments, segment)
	}

	pattern.trailing = strict && trailing && len(pattern.segments) > 0
	pattern.path = pattern.displayPath()
	return pattern, nil
}

func parseRequestPath(rawPath string, strict bool) (requestPath, error) {
	if rawPath == "" || rawPath == "/" {
		return requestPath{}, nil
	}
	path := rawPath
	path = strings.TrimPrefix(path, "/")
	trailing := strings.HasSuffix(path, "/")
	if trailing {
		path = path[:len(path)-1]
	}
	if path == "" {
		return requestPath{trailing: strict && trailing}, nil
	}
	if strings.HasSuffix(path, "/") {
		return requestPath{}, fmt.Errorf("%w: %q contains an empty segment", ErrInvalidRequestPath, rawPath)
	}

	var result requestPath
	result.raw = path
	result.trailing = strict && trailing
	for start := 0; start < len(path); {
		end := strings.IndexByte(path[start:], '/')
		if end < 0 {
			end = len(path)
		} else {
			end += start
		}
		rawSegment := path[start:end]
		if rawSegment == "" {
			return requestPath{}, fmt.Errorf("%w: %q contains an empty segment", ErrInvalidRequestPath, rawPath)
		}
		// 快速校验:含 '%' 的段走完整校验(转义合法 + dot 段);
		// 无转义段只需对潜在 dot 段(长度 ≤2)做检查,长段跳过逐字节校验。
		// fast validation: segments with '%' take the full check (escape
		// validity + dot segments); unescaped segments only need the dot
		// check for short segments, and long segments skip byte-wise
		// validation entirely.
		if strings.IndexByte(rawSegment, '%') >= 0 {
			if err := validateRawSegment(rawSegment); err != nil {
				return requestPath{}, err
			}
		} else if len(rawSegment) <= 2 {
			if rawSegment == "." || rawSegment == ".." {
				return requestPath{}, fmt.Errorf("%w: %q contains dot segment", ErrInvalidRequestPath, rawPath)
			}
		}
		result.segments.Add(pathSegment{start: start, end: end})
		if end == len(path) {
			break
		}
		start = end + 1
	}
	return result, nil
}

// validateRawSegment 校验一段原始路径:转义合法、非 dot 段(含百分号编码形态)。
// validateRawSegment validates one raw segment: valid escapes and no dot segment
// (including percent-encoded forms).
// 语义与旧实现“逐段 PathUnescape 后判定”一致,但不分配。
// semantics match the previous per-segment PathUnescape checks without allocating.
func validateRawSegment(raw string) error {
	var decoded [2]byte
	decodedLen := 0
	dotCandidate := true
	for i := 0; i < len(raw); i++ {
		if raw[i] != '%' {
			if dotCandidate {
				if decodedLen < 2 {
					decoded[decodedLen] = raw[i]
					decodedLen++
				} else {
					dotCandidate = false
				}
			}
			continue
		}
		if i+2 >= len(raw) {
			return fmt.Errorf("%w: %q contains invalid escape", ErrInvalidRequestPath, raw)
		}
		hi, okHi := unhex(raw[i+1])
		lo, okLo := unhex(raw[i+2])
		if !okHi || !okLo {
			return fmt.Errorf("%w: %q contains invalid escape", ErrInvalidRequestPath, raw)
		}
		if dotCandidate {
			if decodedLen < 2 {
				decoded[decodedLen] = byte(hi<<4 | lo)
				decodedLen++
			} else {
				dotCandidate = false
			}
		}
		i += 2
	}
	if dotCandidate &&
		((decodedLen == 1 && decoded[0] == '.') ||
			(decodedLen == 2 && decoded[0] == '.' && decoded[1] == '.')) {
		return fmt.Errorf("%w: %q contains dot segment", ErrInvalidRequestPath, raw)
	}
	return nil
}

// rawSegmentMatches 判断原始段解码后是否等于期望值,不分配。
// rawSegmentMatches reports whether the raw segment decodes to expected without allocating.
func rawSegmentMatches(raw, expected string) bool {
	rawIndex := 0
	for expectedIndex := 0; expectedIndex < len(expected); expectedIndex++ {
		if rawIndex >= len(raw) {
			return false
		}
		var decoded byte
		if raw[rawIndex] == '%' {
			if rawIndex+2 >= len(raw) {
				return false
			}
			hi, okHi := unhex(raw[rawIndex+1])
			lo, okLo := unhex(raw[rawIndex+2])
			if !okHi || !okLo {
				return false
			}
			decoded = byte(hi<<4 | lo)
			rawIndex += 3
		} else {
			decoded = raw[rawIndex]
			rawIndex++
		}
		if decoded != expected[expectedIndex] {
			return false
		}
	}
	return rawIndex == len(raw)
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func splitEscapedPath(rawPath string, sentinel error) ([]string, bool, error) {
	if rawPath == "" {
		rawPath = "/"
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	if rawPath == "/" {
		return nil, false, nil
	}

	body := rawPath[1:]
	trailing := strings.HasSuffix(body, "/")
	if trailing {
		body = body[:len(body)-1]
	}
	if body == "" {
		return nil, trailing, nil
	}

	segments := strings.Split(body, "/")
	for _, segment := range segments {
		if segment == "" {
			return nil, false, fmt.Errorf("%w: %q contains an empty segment", sentinel, rawPath)
		}
	}
	return segments, trailing, nil
}

func parseRouteSegment(rawSegment string, last bool, seen map[string]struct{}) (routeSegment, error) {
	if strings.ContainsAny(rawSegment, "{}") {
		if !strings.HasPrefix(rawSegment, "{") || !strings.HasSuffix(rawSegment, "}") {
			return routeSegment{}, fmt.Errorf("invalid parameter segment %q", rawSegment)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(rawSegment, "{"), "}")
		kind := routeSegmentParameter
		if strings.HasSuffix(name, "...") {
			kind = routeSegmentCatchAll
			name = strings.TrimSuffix(name, "...")
			if !last {
				return routeSegment{}, fmt.Errorf("catch-all parameter %q must be the last segment", rawSegment)
			}
		}
		if !token.IsIdentifier(name) {
			return routeSegment{}, fmt.Errorf("invalid parameter name %q", name)
		}
		if _, exists := seen[name]; exists {
			return routeSegment{}, fmt.Errorf("duplicate parameter name %q", name)
		}
		seen[name] = struct{}{}
		return routeSegment{kind: kind, value: name}, nil
	}
	if strings.HasPrefix(rawSegment, ":") || strings.HasPrefix(rawSegment, "*") {
		return routeSegment{}, fmt.Errorf("unsupported parameter syntax %q", rawSegment)
	}

	decoded, err := url.PathUnescape(rawSegment)
	if err != nil {
		return routeSegment{}, fmt.Errorf("invalid escape in segment %q: %w", rawSegment, err)
	}
	if decoded == "." || decoded == ".." {
		return routeSegment{}, fmt.Errorf("dot segment %q", rawSegment)
	}
	return routeSegment{kind: routeSegmentStatic, value: decoded, rawValue: rawSegment}, nil
}

func (p routePattern) displayPath() string {
	if len(p.segments) == 0 {
		return "/"
	}

	segments := make([]string, len(p.segments))
	for i, segment := range p.segments {
		switch segment.kind {
		case routeSegmentParameter:
			segments[i] = "{" + segment.value + "}"
		case routeSegmentCatchAll:
			segments[i] = "{" + segment.value + "...}"
		default:
			segments[i] = routeDisplayStaticSegment(segment.value)
		}
	}
	path := "/" + strings.Join(segments, "/")
	if p.trailing {
		return path + "/"
	}
	return path
}

// hasParams 报告模式是否包含参数或 catch-all 段。
// hasParams reports whether the pattern contains param or catch-all segments.
func (p routePattern) hasParams() bool {
	for _, segment := range p.segments {
		if segment.kind == routeSegmentParameter || segment.kind == routeSegmentCatchAll {
			return true
		}
	}
	return false
}

func routeDisplayStaticSegment(segment string) string {
	if strings.Contains(segment, "/") {
		return strings.ReplaceAll(segment, "/", "%2F")
	}
	return segment
}

func joinRoutePaths(prefix, route string) string {
	if prefix == "" {
		prefix = "/"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if prefix != "/" {
		prefix = strings.TrimRight(prefix, "/")
	}
	if route == "" {
		return prefix
	}
	if route == "/" {
		if prefix == "/" {
			return "/"
		}
		return prefix + "/"
	}

	route = strings.TrimLeft(route, "/")
	if prefix == "/" {
		return "/" + route
	}
	return prefix + "/" + route
}

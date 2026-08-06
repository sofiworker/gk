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
}

type routePattern struct {
	path     string
	segments []routeSegment
	trailing bool
	strict   bool
}

type requestPath struct {
	segments pathSegmentList
	trailing bool
}

type pathSegmentList struct {
	values   [maxStackPathParams]string
	overflow []string
	len      int
}

func (s *pathSegmentList) Add(value string) {
	if s.len < len(s.values) {
		s.values[s.len] = value
		s.len++
		return
	}
	s.overflow = append(s.overflow, value)
}

func (s pathSegmentList) Len() int {
	return s.len + len(s.overflow)
}

func (s pathSegmentList) At(index int) string {
	if index < s.len {
		return s.values[index]
	}
	return s.overflow[index-s.len]
}

func (s pathSegmentList) JoinFrom(index int) string {
	if index >= s.Len() {
		return ""
	}
	if index == s.Len()-1 {
		return s.At(index)
	}
	var builder strings.Builder
	for current := index; current < s.Len(); current++ {
		if current > index {
			builder.WriteByte('/')
		}
		builder.WriteString(s.At(current))
	}
	return builder.String()
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
		segment, err := url.PathUnescape(rawSegment)
		if err != nil {
			return requestPath{}, fmt.Errorf("%w: %q: %v", ErrInvalidRequestPath, rawPath, err)
		}
		if segment == "." || segment == ".." {
			return requestPath{}, fmt.Errorf("%w: %q contains dot segment", ErrInvalidRequestPath, rawPath)
		}
		result.segments.Add(segment)
		if end == len(path) {
			break
		}
		start = end + 1
	}
	return result, nil
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
	return routeSegment{kind: routeSegmentStatic, value: decoded}, nil
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

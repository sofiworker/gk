package ghttp

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

type routeTerminalKind uint8

const (
	routeTerminalTyped routeTerminalKind = iota
	routeTerminalRaw
	routeTerminalHTTPFunc
	routeTerminalRedirect
	routeTerminalHTML
	routeTerminalSSE
	routeTerminalWebSocket
	routeTerminalStatic
)

type routeDefinition struct {
	method          string
	pattern         routePattern
	handler         http.Handler
	middlewares     []Middleware
	group           *Group
	needsExtractor  bool
	terminal        routeTerminalKind
	responseStatus  int
	responseHeaders []responseHeader
	internal        bool
	doc             RouteDoc
	reqType         reflect.Type
	respType        reflect.Type
	consumes        []string
	produces        []string
}

func (d routeDefinition) clone() routeDefinition {
	cloned := d
	cloned.pattern.segments = append([]routeSegment(nil), d.pattern.segments...)
	cloned.middlewares = append([]Middleware(nil), d.middlewares...)
	cloned.doc = d.doc.clone()
	cloned.consumes = append([]string(nil), d.consumes...)
	cloned.produces = append([]string(nil), d.produces...)
	cloned.responseHeaders = append([]responseHeader(nil), d.responseHeaders...)
	return cloned
}

func (p routePattern) structureKey() string {
	var builder strings.Builder
	builder.Grow(len(p.path) + 8)
	for _, segment := range p.segments {
		builder.WriteByte(byte(segment.kind) + '0')
		builder.WriteByte(':')
		if segment.kind == routeSegmentStatic {
			builder.WriteString(strconv.Itoa(len(segment.value)))
			builder.WriteByte(':')
			builder.WriteString(segment.value)
		}
		builder.WriteByte('/')
	}
	if p.trailing {
		builder.WriteByte('T')
	}
	return builder.String()
}

func (p routePattern) dynamicParameterKeys() []routeDynamicParameter {
	var builder strings.Builder
	builder.Grow(len(p.path) + 8)
	var keys []routeDynamicParameter
	for _, segment := range p.segments {
		builder.WriteByte(byte(segment.kind) + '0')
		builder.WriteByte(':')
		if segment.kind == routeSegmentStatic {
			builder.WriteString(strconv.Itoa(len(segment.value)))
			builder.WriteByte(':')
			builder.WriteString(segment.value)
		}
		builder.WriteByte('/')
		if segment.kind == routeSegmentParameter || segment.kind == routeSegmentCatchAll {
			keys = append(keys, routeDynamicParameter{
				key:  builder.String(),
				name: segment.value,
			})
		}
	}
	return keys
}

type routeDynamicParameter struct {
	key  string
	name string
}

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
	method         string
	pattern        routePattern
	handler        http.Handler
	fastBuild      func(*Server, *Operation) http.HandlerFunc
	middlewares    []Middleware
	group          *Group
	needsExtractor bool
	// requestOnly 标记输入链只读请求元数据;用于无状态快路径判定。
	// requestOnly marks an input chain reading only request metadata; it feeds
	// the stateless fast-path decision.
	requestOnly     bool
	terminal        routeTerminalKind
	responseStatus  int
	responseHeaders []responseHeader
	errorWriter     ErrorWriter
	problemDetails  bool
	internal        bool
	doc             RouteDoc
	reqType         reflect.Type
	respType        reflect.Type
	consumes        []string
	produces        []string
	// openAPI 在路由注册时编译；不可变定义携带反射结果，文档请求无需再次扫描类型。
	// openAPI is compiled while the route is registered; the immutable definition
	// avoids scanning request/response types when the document is served.
	openAPI routeOpenAPIMetadata
}

// routeOpenAPIMetadata 是注册期 endpoint 元数据；registerHandler 填充后视为不可变。
// routeOpenAPIMetadata is registration-time endpoint metadata; values are
// immutable after registerHandler has populated them.
type routeOpenAPIMetadata struct {
	parameters       []*parameter
	bodySchema       interface{}
	bodyContentTypes []string
	bodyRequired     bool
	bodyContent      map[string]any
	responseSchema   interface{}
	responses        map[string]any
}

func (d routeDefinition) clone() routeDefinition {
	cloned := d
	cloned.pattern.segments = append([]routeSegment(nil), d.pattern.segments...)
	cloned.middlewares = append([]Middleware(nil), d.middlewares...)
	cloned.doc = d.doc.clone()
	cloned.consumes = append([]string(nil), d.consumes...)
	cloned.produces = append([]string(nil), d.produces...)
	cloned.responseHeaders = append([]responseHeader(nil), d.responseHeaders...)
	cloned.openAPI = d.openAPI.clone()
	return cloned
}

func (m routeOpenAPIMetadata) clone() routeOpenAPIMetadata {
	cloned := m
	if m.parameters != nil {
		cloned.parameters = make([]*parameter, len(m.parameters))
		for i, p := range m.parameters {
			if p == nil {
				continue
			}
			copyParameter := *p
			copyParameter.Schema = cloneOpenAPIValue(p.Schema)
			cloned.parameters[i] = &copyParameter
		}
	}
	cloned.bodyContentTypes = append([]string(nil), m.bodyContentTypes...)
	cloned.bodySchema = cloneOpenAPIValue(m.bodySchema)
	cloned.bodyContent = cloneSchemaContent(m.bodyContent)
	cloned.responseSchema = cloneOpenAPIValue(m.responseSchema)
	if m.responses != nil {
		cloned.responses = make(map[string]any, len(m.responses))
		for status, response := range m.responses {
			cloned.responses[status] = cloneOpenAPIValue(response)
		}
	}
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

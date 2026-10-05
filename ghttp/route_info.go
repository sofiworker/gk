package ghttp

import (
	"reflect"
	"sort"
)

// RouteKind 表示路由的 handler 形态。
// RouteKind is the handler form of a route.
type RouteKind string

// 路由形态常量。
// Route kind constants.
const (
	RouteEndpoint  RouteKind = "endpoint"
	RouteAction    RouteKind = "action"
	RouteProcedure RouteKind = "procedure"
	RouteRaw       RouteKind = "raw"
)

// RouteDoc 是路由的文档信息，供 OpenAPI 等工具使用，不影响请求处理。
// RouteDoc is a route's documentation, used by tools such as OpenAPI; it does not affect
// request handling.
type RouteDoc struct {
	Summary     string
	Description string
	OperationID string
	Tags        []string
	Deprecated  bool
}

// RouteInfo 描述一个已注册的路由。BodyType/ResultType 为 nil 表示没有 body / 没有结果
// （NoData、Action、Procedure、Raw）。
// RouteInfo describes a registered route. A nil BodyType/ResultType means no body / no
// result (NoData, Action, Procedure, Raw).
type RouteInfo struct {
	Method     string
	Path       string
	Kind       RouteKind
	BodyType   reflect.Type
	ResultType reflect.Type
	Doc        RouteDoc
}

// routeMeta 是构造期记录的类型信息。
// routeMeta is the type information recorded at construction.
type routeMeta struct {
	kind       RouteKind
	bodyType   reflect.Type
	resultType reflect.Type
}

// typeOrNil 返回 T 的反射类型；NoDataType 返回 nil。
// typeOrNil returns T's reflect type, or nil for NoDataType.
func typeOrNil[T any]() reflect.Type {
	t := reflect.TypeFor[T]()
	if t == reflect.TypeFor[NoDataType]() {
		return nil
	}
	return t
}

// WithDoc 设置路由的摘要与描述。
// WithDoc sets the route summary and description.
func WithDoc(summary, description string) Option {
	return func(o *routeOptions) {
		o.doc.Summary, o.doc.Description = summary, description
	}
}

// WithTags 为路由添加标签（可经 Group/Server.With 设为默认值，标签会累加）。
// WithTags adds tags to the route (usable as a Group/Server.With default; tags accumulate).
func WithTags(tags ...string) Option {
	return func(o *routeOptions) { o.doc.Tags = append(o.doc.Tags, tags...) }
}

// WithOperationID 设置路由的 operationId。
// WithOperationID sets the route's operationId.
func WithOperationID(id string) Option {
	return func(o *routeOptions) { o.doc.OperationID = id }
}

// WithDeprecated 把路由标记为已弃用。
// WithDeprecated marks the route deprecated.
func WithDeprecated() Option {
	return func(o *routeOptions) { o.doc.Deprecated = true }
}

// Routes 返回已注册路由的快照，按路径再按方法排序。
// Routes returns a snapshot of the registered routes, sorted by path then method.
func (s *Server) Routes() []RouteInfo {
	s.mu.Lock()
	out := make([]RouteInfo, len(s.routes))
	copy(out, s.routes)
	s.mu.Unlock()
	for i := range out {
		out[i].Doc.Tags = append([]string(nil), out[i].Doc.Tags...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

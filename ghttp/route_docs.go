package ghttp

import (
	"reflect"
	"strings"
)

// ===========================================================================
// 路由文档登记:typed 入口在注册期把自己的输入/输出类型元数据交到这里,OpenAPI 生成
// 据此产出 spec。
//
// 默认零成本红线:未调用 WithOpenAPI 时 collectDocs 为 false,noteRoute 立即返回,
// 既不分配也不持有任何类型信息;命中热路径完全不受影响(本文件的代码只在注册期运行)。
//
// Route documentation registry: typed entries hand their input/output type
// metadata here at registration, and OpenAPI generation turns it into a spec.
//
// The zero-cost default: without WithOpenAPI, collectDocs is false and noteRoute
// returns immediately, allocating nothing and retaining no type information; the
// hit hot path is untouched (everything here runs at registration time only).
// ===========================================================================

// routeDoc 是一个 typed 端点的注册期类型元数据。零值合法:三个字段都可为空(如
// GetNone 无 params 无 body)。
// routeDoc is one typed endpoint's registration-time type metadata. The zero
// value is valid: every field may be empty (e.g. GetNone has neither params nor body).
type routeDoc struct {
	// params 是 params 结构体类型(nil 表示该端点无 params)。
	// params is the params struct type (nil when the endpoint has none).
	params reflect.Type
	// body 是请求体类型(nil 表示无请求体);bodyCT 是其声明的 Content-Type。
	// body is the request-body type (nil when absent); bodyCT is its declared Content-Type.
	body   reflect.Type
	bodyCT string
	// out 是响应契约元数据。
	// out is the response contract metadata.
	out outputDoc
	// raw 标记该端点由 RawHandle 注册:它完全接管响应,没有可供反射的 params/body/output
	// 类型,因此文档只能如实给出"路径与方法存在、响应体形状未声明",不能凭空编造 schema。
	// raw marks an endpoint registered via RawHandle: it fully owns the response and
	// exposes no reflectable params/body/output types, so the document can only state
	// honestly that the path and method exist with an undeclared response shape —
	// never invent a schema.
	raw bool
}

// routeEntry 是登记表的一条:一个 method + 完整路径 + 其类型元数据。
// routeEntry is one registry record: a method plus full path plus type metadata.
type routeEntry struct {
	method string
	// path 是【模板形式】的完整路径(含分组前缀,如 /api/v1/users/{id}),直接用于
	// OpenAPI 的 paths 键——OpenAPI 3 用 {name} 而非 gin 的 :name。
	// path is the full path in TEMPLATE form (including group prefixes, e.g.
	// /api/v1/users/{id}), used directly as an OpenAPI paths key — OpenAPI 3 uses
	// {name}, not gin's :name.
	path string
	doc  routeDoc
	// tags 是分组推导出的 OpenAPI 标签(取分组前缀首段),使同一分组的端点在文档里聚合。
	// tags are OpenAPI tags derived from the group (its prefix's first segment), so
	// endpoints of one group cluster together in the document.
	tags []string
	// summary 是端点摘要，预留给后续能力，当前未被填充。
	// summary is the endpoint summary, reserved for a future capability and not
	// populated today.
	summary string
}

// noteRoute 登记一个 typed 端点的类型元数据。仅在开启 OpenAPI 收集时生效,否则立即返回
// 以保证"不用则零成本"。r 用于取分组前缀(Group 注册的路径是相对本组的)。
// noteRoute records a typed endpoint's type metadata. It is active only when
// OpenAPI collection is enabled, returning immediately otherwise to keep "unused
// means zero cost". r supplies the group prefix (a Group registers paths relative
// to itself).
func (m *mux) noteRoute(r router, method, path string, doc routeDoc) {
	if !m.collectDocs {
		return
	}
	full := path
	var tags []string
	if g, ok := r.(*Group); ok {
		// 必须与 Group.register 走同一个 joinRoutePath:spec 的 paths 键就是"实际注册
		// 的路径模板",两边若各自拼接,前缀带尾斜杠时 spec 会出现 /api//v1/x 这种既非法
		// 又与真实路由不符的键,契约测试反被误导。
		// Must use the same joinRoutePath as Group.register: a spec paths key IS the
		// route template actually registered. Joining independently on each side would
		// put keys like /api//v1/x — illegal and inconsistent with the real route —
		// into the spec whenever a prefix carries a trailing slash, misleading the very
		// contract tests that read it.
		full = joinRoutePath(g.prefix, path)
		if t := tagFromPrefix(g.prefix); t != "" {
			tags = []string{t}
		}
	}
	if full == "" {
		full = "/"
	}
	m.docs = append(m.docs, routeEntry{method: method, path: full, doc: doc, tags: tags})
}

// tagFromPrefix 从分组前缀推导 OpenAPI 标签:取第一个非版本号段(跳过 api、v1 之类),
// 使 /api/v1/users 得到 "users" 而非 "api"。无可用段时返回空串(端点不带标签)。
// tagFromPrefix derives an OpenAPI tag from a group prefix: the first segment that
// is not a version marker (skipping api, v1, and the like), so /api/v1/users
// yields "users" rather than "api". An empty string means the endpoint is untagged.
func tagFromPrefix(prefix string) string {
	for _, seg := range strings.Split(strings.Trim(prefix, "/"), "/") {
		if seg == "" || seg == "api" || isVersionSegment(seg) {
			continue
		}
		if strings.HasPrefix(seg, "{") || strings.HasPrefix(seg, ":") {
			continue // 参数段不是标签 / a parameter segment is not a tag
		}
		return seg
	}
	return ""
}

// isVersionSegment 报告 seg 是否形如 v1 / v2 / v10 的版本段。
// isVersionSegment reports whether seg looks like a v1 / v2 / v10 version segment.
func isVersionSegment(seg string) bool {
	if len(seg) < 2 || (seg[0] != 'v' && seg[0] != 'V') {
		return false
	}
	for i := 1; i < len(seg); i++ {
		if seg[i] < '0' || seg[i] > '9' {
			return false
		}
	}
	return true
}

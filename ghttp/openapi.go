package ghttp

import (
	"context"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ===========================================================================
// OpenAPI 3.1 生成:注册期收集 typed 端点的输入/输出类型,首次请求 spec 时构建一次并
// 缓存字节,此后每次请求只写已缓存的切片。
//
// 默认零成本:未调用 WithOpenAPI 时既不收集元数据、不构建 spec、不注册路由,命中热路径
// 与内存占用都与本能力不存在时完全一致。
//
// OpenAPI 3.1 generation: input/output types of typed endpoints are collected at
// registration, the spec is built once on the first spec request and its bytes
// cached, and later requests only write the cached slice.
//
// Zero cost by default: without WithOpenAPI nothing is collected, no spec is built
// and no route is registered, so the hit hot path and memory footprint are exactly
// as if the capability did not exist.
// ===========================================================================

// OpenAPIInfo 是 spec 的 info 段:服务标识与版本。Title 为空时用 "API",Version 为空
// 时用 "0.0.0"——OpenAPI 要求二者必填。
// OpenAPIInfo is the spec's info block: service identity and version. An empty
// Title becomes "API" and an empty Version becomes "0.0.0" — OpenAPI requires both.
type OpenAPIInfo struct {
	Title       string // 服务名 / service name
	Version     string // 服务版本 / service version
	Description string // 服务描述(可选)/ service description (optional)
}

// OpenAPIServer 是 spec 的 servers 条目:一个基础 URL 及其说明。
// OpenAPIServer is a spec servers entry: one base URL plus its description.
type OpenAPIServer struct {
	URL         string
	Description string
}

// openAPIConfig 是 OpenAPI 能力的配置,经 WithOpenAPI 及其选项写入。
// openAPIConfig is the OpenAPI capability's configuration, written by WithOpenAPI
// and its options.
type openAPIConfig struct {
	info    OpenAPIInfo
	servers []OpenAPIServer
	// route 是暴露 spec 的路径;空串表示只构建不暴露(用户可用 SpecJSON 自行输出)。
	// route is the path exposing the spec; empty means build without exposing (the
	// user can emit it via SpecJSON).
	route string
	// includeErrors 控制是否为每个端点声明标准错误响应(400/415/500 等)。默认 true。
	// includeErrors controls whether standard error responses (400/415/500, …) are
	// declared per endpoint. Default true.
	includeErrors bool
}

// OpenAPIOption 配置 OpenAPI 生成行为。
// OpenAPIOption configures OpenAPI generation behavior.
type OpenAPIOption func(*openAPIConfig)

// WithOpenAPIRoute 设置暴露 spec 的路径(默认 "/openapi.json")。置为空串则不注册路由,
// 只在内存中构建 spec,供 Server.SpecJSON 取用。
// WithOpenAPIRoute sets the path exposing the spec (default "/openapi.json"). An
// empty string registers no route and only builds the spec in memory for
// Server.SpecJSON.
func WithOpenAPIRoute(path string) OpenAPIOption {
	return func(c *openAPIConfig) { c.route = path }
}

// WithOpenAPIServers 声明 spec 的 servers 列表(部署基础 URL)。
// WithOpenAPIServers declares the spec's servers list (deployment base URLs).
func WithOpenAPIServers(servers ...OpenAPIServer) OpenAPIOption {
	return func(c *openAPIConfig) { c.servers = append(c.servers, servers...) }
}

// WithOpenAPIErrorResponses 控制是否为端点声明框架的标准错误响应。默认 true;置 false
// 时 spec 只描述成功响应,文档更精简。
// WithOpenAPIErrorResponses controls whether the framework's standard error
// responses are declared per endpoint. Default true; false leaves the spec
// describing successes only, for a leaner document.
func WithOpenAPIErrorResponses(include bool) OpenAPIOption {
	return func(c *openAPIConfig) { c.includeErrors = include }
}

// WithOpenAPI 开启 OpenAPI 3.1 生成:注册期收集 typed 端点元数据,并默认在
// "/openapi.json" 暴露 spec。它必须在注册路由【之前】应用(即作为 New 的 Option),
// 否则先注册的路由不会被收集。
//
// 未调用本 Option 时,元数据收集与 spec 构建都不发生(默认零成本)。
//
// WithOpenAPI enables OpenAPI 3.1 generation: typed endpoint metadata is collected
// at registration and the spec is exposed at "/openapi.json" by default. It must be
// applied BEFORE routes are registered (i.e. as a New Option); routes registered
// earlier are not collected.
//
// Without this Option neither collection nor spec building happens (zero cost by
// default).
func WithOpenAPI(info OpenAPIInfo, opts ...OpenAPIOption) Option {
	return func(s *Server) {
		cfg := &openAPIConfig{info: info, route: "/openapi.json", includeErrors: true}
		for _, opt := range opts {
			opt(cfg)
		}
		if cfg.info.Title == "" {
			cfg.info.Title = "API"
		}
		if cfg.info.Version == "" {
			cfg.info.Version = "0.0.0"
		}
		s.collectDocs = true
		s.openAPI = cfg
		s.pendingOpenAPIRoute = cfg.route
	}
}

// SpecJSON 返回 OpenAPI 3.1 spec 的 JSON 字节。首次调用时构建并缓存,后续调用返回同一
// 份缓存(spec 在路由注册完成后是不变量)。未开启 WithOpenAPI 时返回 nil。
//
// 返回的切片由 Server 持有,调用方【不得】修改它;需要改写时请自行复制。
//
// SpecJSON returns the OpenAPI 3.1 spec as JSON bytes, built and cached on first
// call and returned from that cache thereafter (the spec is invariant once route
// registration is done). Returns nil when WithOpenAPI was not enabled.
//
// The Server owns the returned slice; callers must NOT modify it — copy it first if
// they need to.
func (s *Server) SpecJSON() []byte {
	if s.openAPI == nil {
		return nil
	}
	s.specOnce.Do(func() { s.specJSON = s.buildSpec() })
	return s.specJSON
}

// registerOpenAPIRoute 注册暴露 spec 的 GET 端点。由 New 在应用完全部 Option 后调用,
// 使 spec 路由本身不出现在 spec 里(它是元数据端点而非业务契约)。
// registerOpenAPIRoute registers the GET endpoint exposing the spec. New calls it
// after all Options are applied, keeping the spec route itself out of the spec (it
// is a metadata endpoint, not a business contract).
func (s *Server) registerOpenAPIRoute() error {
	route := s.pendingOpenAPIRoute
	s.pendingOpenAPIRoute = ""
	if route == "" {
		return nil
	}
	// 注册期间临时关闭收集:spec 端点用 RawHandle 注册,而 RawHandle 现在也会登记路由,
	// 若不关闭,这个元数据端点就会把自己写进业务契约里。
	// Collection is suspended during registration: the spec endpoint is registered
	// via RawHandle, which now records routes too, so leaving it on would let this
	// metadata endpoint document itself as a business contract.
	prev := s.collectDocs
	s.collectDocs = false
	defer func() { s.collectDocs = prev }()
	return s.RawHandle(http.MethodGet, route, func(ctx context.Context, req *Request, resp *Response) error {
		body := s.SpecJSON()
		resp.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.Write(body)
		return nil
	})
}

// ---------------------------------------------------------------------------
// spec 构建 / spec construction
// ---------------------------------------------------------------------------

// pathItem 聚合同一路径下的各 method 操作,按注册顺序记录 method 以稳定输出。
// pathItem groups the operations of one path by method, recording methods in
// registration order for stable output.
type pathItem struct {
	path    string
	methods []string
	ops     map[string]*operation
}

// operation 是一个 method + path 的 OpenAPI 操作。
// operation is one method + path OpenAPI operation.
type operation struct {
	id          string
	summary     string
	tags        []string
	params      []parameter
	requestBody *requestBody
	responses   []response
}

// parameter 是一个 OpenAPI 参数(path/query/header 之一)。
// parameter is one OpenAPI parameter (path, query, or header).
type parameter struct {
	name     string
	in       string
	required bool
	// explode 对数组参数声明"重复出现"风格;ghttp 同时接受逗号分隔,故只作提示。
	// explode declares the "repeated occurrence" style for array parameters; ghttp
	// also accepts comma separation, so it is a hint only.
	explode bool
	s       *schema
}

// requestBody 是请求体声明。
// requestBody is a request-body declaration.
type requestBody struct {
	contentType string
	s           *schema
	required    bool
}

// response 是一个响应声明。
// response is one response declaration.
type response struct {
	status      int
	description string
	contentType string
	s           *schema
}

// buildSpec 遍历注册期收集的路由元数据,构建完整 spec 的 JSON 字节。它在 SpecJSON 的
// sync.Once 内调用,故只执行一次。
// buildSpec walks the route metadata collected at registration and builds the full
// spec's JSON bytes. Called inside SpecJSON's sync.Once, so it runs once.
func (s *Server) buildSpec() []byte {
	reg := newSchemaRegistry()
	items := make(map[string]*pathItem)
	var order []string
	// usedOpIDs 保证 operationId 全局唯一。OpenAPI 要求它在整份文档内唯一,而由
	// method+路径生成的 id 会天然撞车:`GET /users/{id}` 与 `GET /users/by-id` 都归约成
	// getUsersById。冲突时追加序号,而不是让两个操作共用一个 id(那会让代码生成器覆盖
	// 掉其中一个)。
	// usedOpIDs keeps operationId globally unique. OpenAPI requires uniqueness across
	// the document, yet ids derived from method+path naturally collide: both
	// `GET /users/{id}` and `GET /users/by-id` reduce to getUsersById. A numeric suffix
	// is appended on collision rather than letting two operations share one id, which
	// would make code generators overwrite one of them.
	usedOpIDs := make(map[string]bool, len(s.docs))

	for i := range s.docs {
		e := &s.docs[i]
		// 路径键与参数声明必须用同一套模板语法。路由层的 catch-all 写作 {fp...},但
		// OpenAPI 的模板变量语法只有 {fp};原样输出会让路径键与它声明的 fp 参数对不上,
		// 模板非法。
		// The path key and the parameter declarations must share one template syntax.
		// The router spells a catch-all as {fp...}, but OpenAPI's template variable
		// syntax only has {fp}; emitting it verbatim leaves the path key inconsistent
		// with the fp parameter it declares, making the template invalid.
		specPath := specPathTemplate(e.path)
		item, ok := items[specPath]
		if !ok {
			item = &pathItem{path: specPath, ops: make(map[string]*operation)}
			items[specPath] = item
			order = append(order, specPath)
		}
		lower := strings.ToLower(e.method)
		if _, dup := item.ops[lower]; dup {
			continue // 同 method 重复登记(理论上不会发生)/ duplicate method (should not happen)
		}
		item.methods = append(item.methods, lower)
		op := s.buildOperation(reg, e)
		op.id = uniqueOperationID(usedOpIDs, op.id)
		item.ops[lower] = op
	}

	sort.Strings(order)
	return s.renderSpec(reg, items, order)
}

// specPathTemplate 把路由模板转成 OpenAPI 路径模板:catch-all 的 {name...} 归一为 {name}。
// specPathTemplate converts a router template into an OpenAPI path template,
// normalizing a catch-all {name...} to {name}.
func specPathTemplate(p string) string {
	if !strings.Contains(p, "...}") {
		return p
	}
	return strings.ReplaceAll(p, "...}", "}")
}

// uniqueOperationID 在 used 中登记 id,已存在时追加序号后返回。
// uniqueOperationID records id in used, appending a numeric suffix when taken.
func uniqueOperationID(used map[string]bool, id string) string {
	if id == "" {
		id = "operation"
	}
	name := id
	for i := 2; used[name]; i++ {
		name = id + strconv.Itoa(i)
	}
	used[name] = true
	return name
}

// buildOperation 把一条路由元数据翻译成 OpenAPI 操作。
// buildOperation translates one route metadata record into an OpenAPI operation.
func (s *Server) buildOperation(reg *schemaRegistry, e *routeEntry) *operation {
	op := &operation{
		id:      operationID(e.method, e.path),
		summary: e.summary,
		tags:    e.tags,
	}
	if e.doc.params != nil {
		op.params = s.buildParameters(reg, e.doc.params)
	} else if e.doc.raw {
		// raw 端点没有 params 类型可反射,但 OpenAPI 规定路径模板里的每个变量都必须有
		// 对应的 path 参数声明,否则 spec 非法。这里从路径模板本身推出参数(类型只能给
		// string——框架确实不知道 raw handler 会怎么解析它)。
		// A raw endpoint has no params type to reflect, yet OpenAPI requires every
		// variable in a path template to have a matching path parameter declaration
		// or the spec is invalid. Derive them from the template itself (typed as
		// string — the framework genuinely cannot know how the raw handler parses it).
		op.params = pathTemplateParams(e.path)
	}
	if e.doc.body != nil {
		ct := e.doc.bodyCT
		if ct == "" {
			// 契约未声明 Content-Type(自定义解码器可以不声明):文档只能给出一个中性
			// 兜底,不猜测实际格式。
			// The contract declares no Content-Type (a custom decoder may omit it): the
			// document can only state a neutral fallback rather than guess the real format.
			ct = "application/octet-stream"
		}
		op.requestBody = &requestBody{
			contentType: ct,
			s:           reg.schemaFor(e.doc.body, 0),
			required:    true,
		}
	}
	op.responses = s.buildResponses(reg, e)
	return op
}

// buildParameters 依据 params 结构体的绑定计划生成参数列表。它复用与请求期完全相同的
// BindPlan,因此文档与实际绑定行为不会漂移——两者读的是同一份编译结果。
// buildParameters derives the parameter list from the params struct's bind plan. It
// reuses the very same BindPlan the request path uses, so documentation cannot
// drift from actual binding behavior — both read one compiled result.
func (s *Server) buildParameters(reg *schemaRegistry, pt reflect.Type) []parameter {
	plan, err := buildBindPlan(pt)
	if err != nil {
		return nil // 注册期已校验过,理论上不可达 / already validated at registration
	}
	out := make([]parameter, 0, len(plan.steps))
	for i := range plan.steps {
		st := &plan.steps[i]
		in := "query"
		switch st.source {
		case bindSrcPath:
			in = "path"
		case bindSrcHeader:
			in = "header"
		}
		if st.name == bindMapAll {
			continue // 收集全部键的 map 无法用单个具名参数表达 / a collect-all map has no single named parameter
		}
		out = append(out, parameter{
			name: st.name,
			in:   in,
			// path 参数按 OpenAPI 规范必填;query/header 由 handler 自行判断(本框架
			// 不内置校验),故声明为可选。
			// Path parameters are required per the OpenAPI spec; query/header are
			// optional since the handler decides (no built-in validation here).
			required: st.source == bindSrcPath,
			explode:  st.binder.vk == vkSlice,
			s:        reg.paramSchemaFor(st.binder),
		})
	}
	return out
}

// buildResponses 生成响应列表:成功响应来自 OutputSpec 元数据,错误响应按配置追加。
// buildResponses builds the response list: the success response comes from
// OutputSpec metadata, and error responses are appended per configuration.
func (s *Server) buildResponses(reg *schemaRegistry, e *routeEntry) []response {
	if e.doc.raw {
		// RawHandle 端点自行接管响应,注册期没有任何可反射的输出类型。这里只声明
		// "会有一个响应,但形状未由框架声明",既不编造 schema,也不谎称 200 一定是 JSON;
		// 同样不追加 400/415——那些是 typed 绑定与内容协商的产物,raw 端点并不经过它们。
		// A RawHandle endpoint owns its response and exposes no reflectable output
		// type at registration. Declare only "a response exists, but the framework
		// does not declare its shape": neither invent a schema nor claim 200 is
		// necessarily JSON. No 400/415 either — those come from typed binding and
		// content negotiation, which a raw endpoint does not go through.
		out := []response{{
			status:      http.StatusOK,
			description: "Response shape is not declared by the framework (RawHandle endpoint).",
		}}
		if s.openAPI.includeErrors {
			out = append(out, response{
				status:      http.StatusInternalServerError,
				description: genericMessage(http.StatusInternalServerError),
				contentType: "application/json",
				s:           reg.errorSchema(),
			})
		}
		return out
	}
	ok := response{status: e.doc.out.status, description: genericMessage(e.doc.out.status)}
	if ok.status == 0 {
		ok.status = http.StatusOK
	}
	if e.doc.out.hasBody && e.doc.out.typ != nil {
		ok.contentType = e.doc.out.contentType
		if ok.contentType == "" {
			ok.contentType = "application/json"
		}
		ok.s = reg.schemaFor(e.doc.out.typ, 0)
	}
	out := []response{ok}

	if !s.openAPI.includeErrors {
		return out
	}
	// 错误响应引用统一错误体 schema(与 error_chain.go 的实际输出一一对应)。
	// Error responses reference the unified error-body schema (matching what
	// error_chain.go actually writes).
	errRef := reg.errorSchema()
	add := func(status int) {
		out = append(out, response{
			status:      status,
			description: genericMessage(status),
			contentType: "application/json",
			s:           errRef,
		})
	}
	if e.doc.params != nil || e.doc.body != nil {
		add(http.StatusBadRequest)
	}
	if e.doc.body != nil && s.strictContentType && e.doc.bodyCT != "" {
		add(http.StatusUnsupportedMediaType)
	}
	add(http.StatusInternalServerError)
	return out
}

// errorSchema 登记并返回统一错误体的 schema 引用。它描述 error_chain.go 实际写出的
// {"error":{"code","message"}} 形态。
// errorSchema registers and returns the unified error body's schema reference,
// describing the {"error":{"code","message"}} shape error_chain.go actually writes.
func (r *schemaRegistry) errorSchema() *schema {
	// 名字在注册器创建时就已预留(见 newSchemaRegistry),因此这里 r.errorName 必定是一个
	// 未被用户类型占用的名字。若沿用硬编码的 "Error" 并在用户业务类型恰好也叫 Error 且
	// 先注册时,400/500 的 $ref 会指向用户 schema —— 框架的错误契约被静默替换成业务结构,
	// 那是最难发现的一类文档谎言。
	// The name is reserved when the registry is created (see newSchemaRegistry), so
	// r.errorName is guaranteed not to collide with a user type. Hardcoding "Error"
	// instead would make the 400/500 $ref point at a user schema whenever a business
	// type is also named Error and registers first — silently replacing the framework's
	// error contract with a business shape, the hardest kind of doc lie to notice.
	name := r.errorName
	if _, ok := r.defs[name]; !ok {
		r.defs[name] = &schema{
			typ: "object",
			properties: []schemaProp{{name: "error", s: &schema{
				typ: "object",
				properties: []schemaProp{
					{name: "code", s: &schema{typ: "string", description: "Stable machine-readable error code"}},
					{name: "message", s: &schema{typ: "string", description: "Human-readable error message"}},
				},
				required: []string{"code", "message"},
			}}},
			required: []string{"error"},
		}
	}
	return &schema{ref: name}
}

// pathTemplateParams 从路径模板中提取 {name} 变量,生成必填的 string 型 path 参数。
// 供没有 params 类型可反射的 raw 端点使用,以保证 spec 合法(OpenAPI 要求模板变量必须
// 有对应声明)。catch-all 的 {name...} 归一为 name。
// pathTemplateParams extracts {name} variables from a path template as required
// string path parameters. Used for raw endpoints with no reflectable params type, so
// the spec stays valid (OpenAPI requires every template variable to be declared).
// A catch-all {name...} is normalized to name.
func pathTemplateParams(tmpl string) []parameter {
	var out []parameter
	for _, seg := range strings.Split(tmpl, "/") {
		if len(seg) < 2 || seg[0] != '{' || seg[len(seg)-1] != '}' {
			continue
		}
		name := strings.TrimSuffix(seg[1:len(seg)-1], "...")
		if name == "" {
			continue
		}
		out = append(out, parameter{
			name:     name,
			in:       "path",
			required: true,
			s:        &schema{typ: "string"},
		})
	}
	return out
}

// operationID 由 method 与路径生成稳定的 operationId(如 getUsersById)。
// operationID builds a stable operationId from the method and path (e.g. getUsersById).
func operationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, "{") {
			// {id} → ById,让参数化段在 id 里可读。
			// {id} → ById so parameterized segments read well in the id.
			b.WriteString("By")
			b.WriteString(capitalizeASCII(strings.Trim(seg, "{}.")))
			continue
		}
		b.WriteString(capitalizeASCII(seg))
	}
	return b.String()
}

// capitalizeASCII 把首字母大写并去掉路径里不适合出现在标识符中的字符。
// capitalizeASCII upper-cases the first letter and drops characters unsuitable for
// an identifier.
func capitalizeASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	upper := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			if upper {
				c -= 'a' - 'A'
				upper = false
			}
			b.WriteByte(c)
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			upper = false
			b.WriteByte(c)
		default:
			upper = true // 分隔符:下一个字母大写 / separator: capitalize the next letter
		}
	}
	return b.String()
}

// specOnceHolder 让 Server 的 spec 缓存字段集中声明,避免在 server.go 里散落 OpenAPI 细节。
// specOnceHolder keeps Server's spec cache fields declared together, avoiding
// OpenAPI details scattered across server.go.
type specOnceHolder struct {
	specOnce sync.Once
	specJSON []byte
}

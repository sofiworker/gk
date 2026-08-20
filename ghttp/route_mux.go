package ghttp

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type routeMatchKind uint8

const (
	routeMatchFound routeMatchKind = iota
	routeMatchNotFound
	routeMatchMethodNotAllowed
	routeMatchBadPath
)

type routeMatchResult struct {
	kind         routeMatchKind
	route        *compiledRoute
	allow        []string
	suppressBody bool
	// path 是匹配过程中收集的段偏移,供参数提取复用;badPath 非空时请求
	// 路径非法(空段/非法转义/dot 段),kind 为 routeMatchBadPath。
	// path holds segment offsets collected while matching for param
	// extraction; a non-nil badPath marks an invalid request path (empty
	// segment, bad escape, or dot segment).
	path    requestPath
	badPath error
	// params 在匹配命中时由 fillMatchedParams 一次性填充,避免后续
	// 独立的 extractMatched 二次遍历。
	// params is filled once by fillMatchedParams on match success, avoiding
	// the separate extractMatched second pass.
	params pathParamList
}

type paramSlot struct {
	name string
	// index 是 pattern 段索引,与记录段列表索引一致(pattern 遍历顺序)。
	// index is the pattern segment index, identical to the recorded segment
	// list index (pattern traversal order).
	index    int
	catchAll bool
}

type compiledRoute struct {
	definition routeDefinition
	// directName 记录"静态前缀 + 单末尾参数"直达索引的参数名。
	// directName holds the param name of the direct single-trailing-param index.
	directName string
	// paramPos 记录参数名到模式段索引的映射,供惰性解码按 key 定位。
	// paramPos maps a param name to its pattern segment index for lazy decoding.
	paramPos map[string]int
	// paramSlots 按模式段序排列的参数槽位,用于 match 命中后一次性填充 params
	// 列表,避免独立 extractMatched 的全量遍历。
	// paramSlots lists param slots in pattern-segment order, used by
	// fillMatchedParams for a single-pass params fill after a match hit.
	paramSlots []paramSlot
	// compiledHandlers 是重写后的切片链:中间件在前、终端殿后,注册期固化。
	// compiledHandlers is the rewritten slice chain: middleware first,
	// terminal last, frozen at registration.
	compiledHandlers []HandlerFunc
	// needsState 标记终端需要经 ctxFromRequest 读 Ctx 状态(body/form 缓存),
	// dispatch 需把 Ctx 挂到请求 context(每请求一次)。
	// needsState marks terminals that read Ctx state (body/form caches) via
	// ctxFromRequest, so dispatch attaches the Ctx to the request context
	// (once per request).
	needsState bool
	// direct 是注册期直编的免 Ctx 终端:无中间件、无参数、无错误模型的
	// NoInput+JSON/Text 路由直接把 handler 编译成 http.HandlerFunc,dispatch
	// 命中后绕过 Ctx 池与切片链(HEAD 抑制由路由方法保证——仅 GET 直编)。
	// direct is the registration-compiled Ctx-free terminal: middleware-less,
	// param-less, error-model-less NoInput+JSON/Text routes compile their
	// handler into a plain http.HandlerFunc, so dispatch skips the Ctx pool
	// and slice chain on a hit (HEAD suppression is guaranteed by the route
	// method — direct only applies to GET).
	direct http.HandlerFunc
	// directParamHandler 是单参数 stateIndependent 路由的免 Ctx 终端:
	// dispatch 在直达索引命中时把参数值直接传入,绕过 Ctx 池、params 填充
	// 与切片链。仅无中间件、无错误模型的 GET 路由设置。
	// directParamHandler is the Ctx-free terminal for single-param
	// stateIndependent routes: dispatch passes the param value straight in,
	// skipping the Ctx pool, params fill and slice chain. Set only for GET
	// routes with no middleware and no error model.
	directParamHandler func(http.ResponseWriter, *http.Request, string)
}

// fillMatchedParams 基于匹配期记录的段偏移一次性填充参数:只遍历参数槽位
// (paramSlots),跳过静态段;无 % 转义时零拷贝。catchAll 段从槽位段起拼接。
// fillMatchedParams fills params in one pass over param slots only, using
// the segment offsets recorded during matching; static segments are skipped
// and segments without '%' are returned zero-copy. catchAll joins from its
// slot segment onward.
func (r *compiledRoute) fillMatchedParams(raw string, segments *pathSegmentList) (pathParamList, error) {
	var params pathParamList
	// 超栈内槽位时预分配溢出切片,避免 append 逐次扩容与拷贝。
	// 容量精确等于溢出槽位数,fill 恰好填满,绝不触发 append 扩容:
	// 这保证本列表独占底层数组,后续按值复制(matchedParams)共享的
	// overflow 头只读安全——任何再 Add 都会重分配而不是别名写入。
	// pre-size the overflow slice when exceeding inline slots to avoid
	// incremental append reallocations and copies. Capacity exactly equals
	// the overflow slot count and the fill uses it all without growing:
	// the list owns its backing array, so by-value copies (matchedParams)
	// share a read-only overflow header — any later Add reallocates instead
	// of aliasing a write.
	if len(r.paramSlots) > maxStackPathParams {
		params.overflow = make([]pathParam, 0, len(r.paramSlots)-maxStackPathParams)
	}
	for _, slot := range r.paramSlots {
		var value string
		if slot.catchAll {
			value = rawJoinFromSegments(raw, segments, slot.index)
		} else {
			segment := segments.At(slot.index)
			if segment.start < 0 || segment.start > segment.end || segment.end > len(raw) {
				return params, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, r.definition.pattern.path)
			}
			value = raw[segment.start:segment.end]
		}
		if strings.IndexByte(value, '%') >= 0 {
			decoded, err := url.PathUnescape(value)
			if err != nil {
				return params, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, r.definition.pattern.path)
			}
			value = decoded
		}
		params.Add(slot.name, value)
	}
	return params, nil
}

// rawJoinFromSegments 返回自 index 段起的原始段连接(保留 '/' 分隔,未解码),
// 与 requestPath.RawJoinFrom 语义一致。
// rawJoinFromSegments joins raw segments from index onward, keeping '/'
// separators, matching requestPath.RawJoinFrom semantics.
func rawJoinFromSegments(raw string, segments *pathSegmentList, index int) string {
	if index >= segments.Len() {
		return ""
	}
	first := segments.At(index)
	last := segments.At(segments.Len() - 1)
	if first.start < 0 || first.start > last.end || last.end > len(raw) {
		return ""
	}
	return raw[first.start:last.end]
}

func (r *compiledRoute) extract(path requestPath) (pathParamList, error) {
	if r == nil {
		return pathParamList{}, fmt.Errorf("%w: route is nil", ErrInvalidRequestPath)
	}
	pattern := r.definition.pattern
	if pattern.trailing != path.trailing {
		return pathParamList{}, fmt.Errorf("%w: trailing slash does not match %s", ErrInvalidRequestPath, pattern.path)
	}

	var params pathParamList
	segmentIndex := 0
	for _, segment := range pattern.segments {
		switch segment.kind {
		case routeSegmentStatic:
			if segmentIndex >= path.segments.Len() || !rawSegmentMatches(path.RawAt(segmentIndex), segment.value) {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			segmentIndex++
		case routeSegmentParameter:
			if segmentIndex >= path.segments.Len() {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			value, err := path.DecodeAt(segmentIndex)
			if err != nil {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			params.Add(segment.value, value)
			segmentIndex++
		case routeSegmentCatchAll:
			value, err := url.PathUnescape(path.RawJoinFrom(segmentIndex))
			if err != nil {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			params.Add(segment.value, value)
			segmentIndex = path.segments.Len()
		}
	}
	if segmentIndex != path.segments.Len() {
		return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
	}
	return params, nil
}

// validate 校验请求路径是否仍匹配路由结构,不解码参数值。
// validate checks the request path against the route structure without decoding values.
// 用于 extractorTerminal 的防御性回退:中间件替换 context 后重新校验已匹配路径。
// used by the extractorTerminal defensive fallback to re-check the matched path.
func (r *compiledRoute) validate(path requestPath) error {
	if r == nil {
		return fmt.Errorf("%w: route is nil", ErrInvalidRequestPath)
	}
	pattern := r.definition.pattern
	if pattern.trailing != path.trailing {
		return fmt.Errorf("%w: trailing slash does not match %s", ErrInvalidRequestPath, pattern.path)
	}
	segmentIndex := 0
	for _, segment := range pattern.segments {
		switch segment.kind {
		case routeSegmentStatic:
			if segmentIndex >= path.segments.Len() || !rawSegmentMatches(path.RawAt(segmentIndex), segment.value) {
				return fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			segmentIndex++
		case routeSegmentParameter:
			if segmentIndex >= path.segments.Len() {
				return fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			segmentIndex++
		case routeSegmentCatchAll:
			segmentIndex = path.segments.Len()
		}
	}
	if segmentIndex != path.segments.Len() {
		return fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
	}
	return nil
}

type routeMux struct {
	root                routeMuxNode
	routes              []*compiledRoute
	staticByMethod      map[string]map[string]*compiledRoute
	staticGet           map[string]*compiledRoute
	directParamByMethod map[string]map[string]*compiledRoute
	directParamGet      map[string]*compiledRoute
	// hasDynamic 为 true 时路由表包含动态段(参数/通配/catch-all),false 时
	// 全为静态路由,可跳过动态匹配与树遍历。
	// hasDynamic is true when the route table contains dynamic segments
	// (params/wildcards/catch-alls); false means all static, allowing the
	// dynamic match and tree traversal to be skipped.
	hasDynamic bool
	// staticPathAllows 是静态路径→方法列表的注册期预计算映射,用于纯静态
	// 路由表 405 响应的 Allow 头(已排序,含 GET→HEAD 派生)。
	// staticPathAllows is a registration-time pre-computed map of static
	// path → allowed methods, used for the Allow header in 405 responses on
	// purely-static route tables (sorted, with GET→HEAD derivation).
	staticPathAllows map[string]string
}

// routeStaticChild 是静态子节点:segment 为注册段的原始(转义)文本片段。
// routeStaticChild is a static child; segment is the raw registered text.
type routeStaticChild struct {
	segment string
	node    *routeMuxNode
}

// routeMuxNode 是 radix 前缀合并树节点。静态段文本经 rawValue 合并(注册原文,
// 不含 '/'),宽节点(≥4 且首字节互异)建 [256]int16 首字节分派表。
// routeMuxNode is a radix-prefix-merged tree node. Static segment text is
// merged on rawValue (registration text, never containing '/'); wide nodes
// (>=4 children with distinct first bytes) build a [256]int16 dispatch table.
type routeMuxNode struct {
	children  []routeStaticChild
	idx       *[256]int16
	parameter *routeMuxNode
	catchAll  *routeMuxNode
	routes    routeMethodSet
	trailing  routeMethodSet
}

// maxCollectedMethods 覆盖全部标准方法 + HEAD 派生,允许栈内收集。
// maxCollectedMethods covers every standard method plus the HEAD derivation,
// so miss-path collection stays on the stack.
const maxCollectedMethods = 16

type routeMethodSet struct {
	routes []*compiledRoute
}

// methodCollector 在 miss 遍历中收集候选方法(栈数组,零堆分配)。
// methodCollector gathers candidate methods during the miss traversal on a
// stack array with zero heap allocation.
type methodCollector struct {
	methods [maxCollectedMethods]string
	len     int
}

func (c *methodCollector) add(method string) {
	for i := 0; i < c.len; i++ {
		if c.methods[i] == method {
			return
		}
	}
	if c.len < len(c.methods) {
		c.methods[c.len] = method
		c.len++
	}
}

func (c *methodCollector) addSet(set *routeMethodSet) {
	for _, route := range set.routes {
		c.add(route.definition.method)
	}
}

func (s *routeMethodSet) add(route *compiledRoute) {
	for _, current := range s.routes {
		if current.definition.method == route.definition.method {
			return
		}
	}
	s.routes = append(s.routes, route)
}

func (s *routeMethodSet) find(method string) *compiledRoute {
	for _, route := range s.routes {
		if route.definition.method == method {
			return route
		}
	}
	return nil
}

func newRouteMux(definitions []routeDefinition) *routeMux {
	mux := &routeMux{}
	for _, definition := range definitions {
		route := &compiledRoute{definition: definition.clone()}
		route.paramPos = make(map[string]int, len(definition.pattern.segments))
		for index, segment := range definition.pattern.segments {
			switch segment.kind {
			case routeSegmentParameter:
				route.paramPos[segment.value] = index
				route.paramSlots = append(route.paramSlots, paramSlot{name: segment.value, index: index})
			case routeSegmentCatchAll:
				route.paramPos[segment.value] = index
				route.paramSlots = append(route.paramSlots, paramSlot{name: segment.value, index: index, catchAll: true})
			}
		}
		mux.routes = append(mux.routes, route)
		mux.root.insert(definition.pattern.segments, route)
		if definition.pattern.hasParams() {
			mux.hasDynamic = true
			mux.addDirectParam(route)
		} else {
			// 静态路由保留 O(1) 直达索引:静态命中比树遍历更快。
			// static routes keep an O(1) direct index: static hits are
			// faster than tree traversal.
			if mux.staticByMethod == nil {
				mux.staticByMethod = make(map[string]map[string]*compiledRoute)
			}
			byPath := mux.staticByMethod[definition.method]
			if byPath == nil {
				byPath = make(map[string]*compiledRoute)
				mux.staticByMethod[definition.method] = byPath
			}
			byPath[definition.pattern.path] = route
			if definition.method == http.MethodGet {
				if mux.staticGet == nil {
					mux.staticGet = make(map[string]*compiledRoute)
				}
				mux.staticGet[definition.pattern.path] = route
			}
		}
	}
	mux.root.freeze()
	mux.buildStaticPathAllows()
	return mux
}

// buildStaticPathAllows 为所有静态路径预计算 Allow 头(注册期)。收集各路径
// 的注册方法,派生 GET→HEAD,排序后存入 staticPathAllows。
// buildStaticPathAllows pre-computes the Allow header for every static path
// at registration time. It collects each path's registered methods, derives
// GET→HEAD, sorts, and stores the result in staticPathAllows.
func (m *routeMux) buildStaticPathAllows() {
	collected := make(map[string]*methodCollector)
	for _, route := range m.routes {
		if route.definition.pattern.hasParams() {
			continue
		}
		path := route.definition.pattern.path
		c, ok := collected[path]
		if !ok {
			c = &methodCollector{}
			collected[path] = c
		}
		c.add(route.definition.method)
	}
	if len(collected) == 0 {
		return
	}
	m.staticPathAllows = make(map[string]string, len(collected))
	for path, c := range collected {
		for i := 0; i < c.len; i++ {
			if c.methods[i] == http.MethodGet {
				c.add(http.MethodHead)
				break
			}
		}
		methods := append([]string(nil), c.methods[:c.len]...)
		sort.Strings(methods)
		m.staticPathAllows[path] = strings.Join(methods, ", ")
	}
}

// insert 按模式段递归插入;静态段文本用 rawValue 做 radix 前缀合并。
// insert inserts a route by pattern segments; static text merges on rawValue.
func (n *routeMuxNode) insert(segments []routeSegment, route *compiledRoute) {
	if len(segments) == 0 {
		if route.definition.pattern.trailing {
			n.trailing.add(route)
			return
		}
		n.routes.add(route)
		return
	}
	segment := segments[0]
	rest := segments[1:]
	switch segment.kind {
	case routeSegmentParameter:
		if n.parameter == nil {
			n.parameter = &routeMuxNode{}
		}
		n.parameter.insert(rest, route)
		return
	case routeSegmentCatchAll:
		if n.catchAll == nil {
			n.catchAll = &routeMuxNode{}
		}
		n.catchAll.insert(rest, route)
		return
	}

	text := segment.rawValue
	for i := range n.children {
		child := &n.children[i]
		common := commonPrefix(text, child.segment)
		if common == 0 {
			continue
		}
		if common == len(child.segment) {
			rem := rest
			if common < len(text) {
				rem = append([]routeSegment{{kind: routeSegmentStatic, rawValue: text[common:]}}, rest...)
			}
			child.node.insert(rem, route)
			return
		}
		// 分裂子节点:公共前缀保留,尾巴成为孙节点。parameter/catchAll 与
		// 终端集随原节点走,当前节点自身的子树不动。
		// split the child: the shared prefix stays and the tail becomes a
		// grandchild. parameter/catchAll children and terminal sets move with
		// the original child; the current node's own subtrees stay put.
		grand := &routeMuxNode{
			children:  child.node.children,
			idx:       child.node.idx,
			parameter: child.node.parameter,
			catchAll:  child.node.catchAll,
			routes:    child.node.routes,
			trailing:  child.node.trailing,
		}
		*child = routeStaticChild{
			segment: child.segment[:common],
			node: &routeMuxNode{
				children: []routeStaticChild{{segment: child.segment[common:], node: grand}},
			},
		}
		rem := rest
		if common < len(text) {
			rem = append([]routeSegment{{kind: routeSegmentStatic, rawValue: text[common:]}}, rest...)
		}
		child.node.insert(rem, route)
		return
	}
	n.children = append(n.children, routeStaticChild{segment: text, node: &routeMuxNode{}})
	n.children[len(n.children)-1].node.insert(rest, route)
}

// freeze 递归构建首字节分派表:宽节点(≥4 且首字节互异)匹配 O(1) 跳转。
// freeze builds the first-byte dispatch tables: wide nodes with distinct
// first bytes jump in O(1).
func (n *routeMuxNode) freeze() {
	for i := range n.children {
		n.children[i].node.freeze()
	}
	if n.parameter != nil {
		n.parameter.freeze()
	}
	if n.catchAll != nil {
		n.catchAll.freeze()
	}
	n.idx = nil
	if len(n.children) < 4 {
		return
	}
	var table [256]int16
	for i := range table {
		table[i] = -1
	}
	for i := range n.children {
		first := n.children[i].segment[0]
		if table[first] >= 0 {
			return
		}
		table[first] = int16(i)
	}
	n.idx = &table
}

// matchStatic 尝试未转义静态路径的冻结直达索引。
// matchStatic attempts the frozen direct index for an unescaped static path.
// 若需要动态树决定结果（包括静态路径只为其他方法存在）则返回 false。
// It returns false when the dynamic tree must decide the result, including a
// static path that exists only for another method.
func (m *routeMux) matchStatic(method, path string) (routeMatchResult, bool) {
	if m == nil || m.staticByMethod == nil {
		return routeMatchResult{}, false
	}
	if strings.IndexByte(path, '%') >= 0 {
		return routeMatchResult{}, false
	}
	// GET 是绝对多数方法:跳过 method 索引,单次 staticGet 直达。
	// GET dominates request traffic: skip the method index and hit staticGet
	// with a single lookup.
	if method == http.MethodGet {
		if route := m.staticGet[path]; route != nil {
			return routeMatchResult{kind: routeMatchFound, route: route}, true
		}
		return routeMatchResult{}, false
	}
	byPath := m.staticByMethod[method]
	if byPath == nil {
		if method != http.MethodHead {
			return routeMatchResult{}, false
		}
		// HEAD 回退 GET:无 HEAD 注册路由时按 GET 处理并抑制响应体。
		// HEAD falls back to GET: suppress the body when no HEAD route exists.
		if route := m.staticGet[path]; route != nil {
			return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: true}, true
		}
		return routeMatchResult{}, false
	}
	if route := byPath[path]; route != nil {
		return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: method == http.MethodHead}, true
	}
	if method == http.MethodHead {
		if route := m.staticGet[path]; route != nil {
			return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: true}, true
		}
	}
	return routeMatchResult{}, false
}

// addDirectParam 为“静态前缀 + 单个末尾参数”的直达终结器建立冻结索引。
// addDirectParam indexes direct terminals shaped as a static prefix followed by
// one final parameter.
func (m *routeMux) addDirectParam(route *compiledRoute) {
	if m == nil || route == nil {
		return
	}
	pattern := route.definition.pattern
	if pattern.trailing || len(pattern.segments) == 0 {
		return
	}
	last := pattern.segments[len(pattern.segments)-1]
	if last.kind != routeSegmentParameter {
		return
	}
	var prefix strings.Builder
	prefix.Grow(len(pattern.path))
	prefix.WriteByte('/')
	for index, segment := range pattern.segments[:len(pattern.segments)-1] {
		if segment.kind != routeSegmentStatic || segment.rawValue != segment.value || strings.ContainsRune(segment.value, '/') {
			return
		}
		if index > 0 {
			prefix.WriteByte('/')
		}
		prefix.WriteString(segment.value)
	}
	if len(pattern.segments) > 1 {
		prefix.WriteByte('/')
	}
	if m.directParamByMethod == nil {
		m.directParamByMethod = make(map[string]map[string]*compiledRoute)
	}
	byPrefix := m.directParamByMethod[route.definition.method]
	if byPrefix == nil {
		byPrefix = make(map[string]*compiledRoute)
		m.directParamByMethod[route.definition.method] = byPrefix
	}
	route.directName = last.value
	key := prefix.String()
	byPrefix[key] = route
	if route.definition.method == http.MethodGet {
		if m.directParamGet == nil {
			m.directParamGet = make(map[string]*compiledRoute)
		}
		m.directParamGet[key] = route
	}
}

// matchDirectParam 匹配未转义的简单单参数路径；其他形态返回 false 走完整树。
// matchDirectParam matches an unescaped simple single-parameter path; all other
// shapes return false and fall back to the full tree.
func (m *routeMux) matchDirectParam(method, path string) (*compiledRoute, string, bool) {
	if m == nil || m.directParamByMethod == nil || method == http.MethodHead || path == "" {
		return nil, "", false
	}
	slash := strings.LastIndexByte(path, '/')
	if slash < 0 || slash == len(path)-1 {
		return nil, "", false
	}
	value := path[slash+1:]
	if value == "." || value == ".." {
		return nil, "", false
	}
	if method == http.MethodGet {
		route := m.directParamGet[path[:slash+1]]
		return route, value, route != nil
	}
	byPrefix := m.directParamByMethod[method]
	if byPrefix == nil {
		return nil, "", false
	}
	route := byPrefix[path[:slash+1]]
	return route, value, route != nil
}

// walkOnce 构建一次遍历状态并从根节点扫描。每次调用先把段表/值表回零(HEAD
// 的两次查找复用同一批栈结构,失败路径的成对 Truncate 已保证清空,这里的回退
// 是防御性的,只花两次长度写)。显式指针参数(而非闭包捕获)让逃逸分析把
// allow/segments/paramValues 全部留在 match 的栈帧上。
// walkOnce builds one walk state and scans from the root. Each call first
// rewinds the segment/value tables to zero (both HEAD lookups reuse the same
// stack structures; the failure paths already rewind them in pairs, so the
// reset here is defensive and costs two length writes). Explicit pointer
// parameters (instead of closure captures) let escape analysis keep
// allow/segments/paramValues on match's stack frame.
func (m *routeMux) walkOnce(candidate string, scanPath string, strict bool, allow *methodCollector, segments *pathSegmentList, paramValues *paramValueList) (*compiledRoute, error) {
	segments.Truncate(0)
	paramValues.Truncate(0)
	w := routeWalkState{
		method:   candidate,
		strict:   strict,
		allow:    allow,
		segments: segments,
		params:   paramValues,
	}
	// raw 以值参数传递:若放进状态结构,其字符串内容流入参数值表的溢出
	// append 会被判定为"状态内容泄漏",连带 allow/segments/paramValues
	// 全部上堆。字符串按值走寄存器,无此牵连。
	// raw passes as a value parameter: stored in the state, its string
	// content would flow into the value table's overflow append, marking
	// the state as content-leaking and dragging allow/segments/paramValues
	// onto the heap. A string by value rides in registers without that.
	return m.root.lookupAllow(scanPath, 0, 0, &w)
}

// match 扫描原始请求路径:radix 树一次遍历完成匹配、段偏移收集、内联参数值
// 收集与 405 方法收集。路径非法(空段/非法转义/dot 段)返回 routeMatchBadPath。
// match scans the raw request path: one radix traversal matches, collects
// segment offsets and inline param values, and gathers 405 methods. Invalid
// paths (empty segments, bad escapes, dot segments) return routeMatchBadPath.
func (m *routeMux) match(method, rawPath string, strict bool) routeMatchResult {
	if rawPath == "" {
		rawPath = "/"
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	// 扫描路径去掉前导 '/';根路径规范化为空段序列,尾斜杠判定在
	// lookupAllow 内完成。
	// strip the leading '/' from the scan path; the root path normalizes to
	// an empty segment sequence, and trailing-slash detection happens inside
	// lookupAllow.
	scanPath := rawPath[1:]

	var allow methodCollector
	var segments pathSegmentList
	var paramValues paramValueList
	// walkOnce 用显式指针参数而非闭包:闭包按引用捕获会把
	// allow/segments/paramValues 逼到堆上(每请求 3 次分配)。
	// walkOnce takes explicit pointers instead of a closure: by-reference
	// closure capture would force allow/segments/paramValues onto the heap
	// (three allocations per request).
	var route *compiledRoute
	var err error
	suppressBody := method == http.MethodHead
	if method == http.MethodHead {
		if route, err = m.walkOnce(http.MethodHead, scanPath, strict, &allow, &segments, &paramValues); route == nil && err == nil {
			route, err = m.walkOnce(http.MethodGet, scanPath, strict, &allow, &segments, &paramValues)
		}
	} else {
		route, err = m.walkOnce(method, scanPath, strict, &allow, &segments, &paramValues)
	}
	if err != nil {
		return routeMatchResult{kind: routeMatchBadPath, badPath: err}
	}
	if route != nil {
		result := routeMatchResult{
			kind:         routeMatchFound,
			route:        route,
			suppressBody: suppressBody,
			path: requestPath{
				// raw 与段偏移基准一致(均相对 scanPath);提取器只依赖两者
				// 的相对一致性。
				// raw shares the scanPath base with the segment offsets; the
				// extractors only rely on their relative consistency.
				raw:      scanPath,
				segments: segments,
				trailing: strict && len(scanPath) > 0 && strings.HasSuffix(scanPath, "/"),
			},
		}
		// 遍历已按槽位序内联收集参数值:计数一致时直接命名映射,免去
		// fillMatchedParams 的段 At/边界检查/'%' 重扫二次遍历;catchAll 空余量
		// 等形态计数不一致,回退到段偏移填充。
		// the walk already collected param values inline in slot order: with
		// matching counts the named mapping skips fillMatchedParams' second
		// pass of segment At/bounds checks/'%' rescans; shapes like an empty
		// catchAll remainder mismatch the count and fall back to the
		// offset-based fill.
		if len(route.paramSlots) > 0 {
			if paramValues.Len() == len(route.paramSlots) {
				params := pathParamList{}
				if len(route.paramSlots) > maxStackPathParams {
					params.overflow = make([]pathParam, 0, len(route.paramSlots)-maxStackPathParams)
				}
				for i, slot := range route.paramSlots {
					value := paramValues.At(i)
					if value.escaped {
						decoded, decodeErr := url.PathUnescape(value.value)
						if decodeErr != nil {
							return routeMatchResult{kind: routeMatchBadPath, badPath: fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, route.definition.pattern.path)}
						}
						params.Add(slot.name, decoded)
						continue
					}
					params.Add(slot.name, value.value)
				}
				result.params = params
			} else {
				params, fillErr := route.fillMatchedParams(scanPath, &segments)
				if fillErr != nil {
					return routeMatchResult{kind: routeMatchBadPath, badPath: fillErr}
				}
				result.params = params
			}
		}
		return result
	}
	if allow.len == 0 {
		// 兜底校验:miss 路径仍需区分 400(非法段)与 404。
		// fallback validation: the miss path must still tell 400 from 404.
		if err := validateRawPath(rawPath); err != nil {
			return routeMatchResult{kind: routeMatchBadPath, badPath: err}
		}
		return routeMatchResult{kind: routeMatchNotFound}
	}
	// GET 派生 HEAD,与历史 allowedMethods 语义一致。
	// derive HEAD from GET, matching the historical allowedMethods semantics.
	for i := 0; i < allow.len; i++ {
		if allow.methods[i] == http.MethodGet {
			allow.add(http.MethodHead)
			break
		}
	}
	methods := append([]string(nil), allow.methods[:allow.len]...)
	sort.Strings(methods)
	return routeMatchResult{kind: routeMatchMethodNotAllowed, allow: methods}
}

// routeWalkState 聚合遍历热状态:方法/原始路径/strict/allow/段偏移表与内联
// 参数值表。递归只传 (pos, segStart, w),替代原先 7 个参数的逐层流量。
// routeWalkState bundles the walk's hot state: method, raw path, strict,
// allow, the segment offset table and the inline param value table. The
// recursion passes only (pos, segStart, w), replacing the per-level traffic
// of the former seven arguments.
type routeWalkState struct {
	method   string
	strict   bool
	allow    *methodCollector
	segments *pathSegmentList
	params   *paramValueList
}

// lookupAllow 扫描 raw[pos:]:静态段整段 memcmp(首字节分派),参数段单遍扫描
// 定界并顺带校验、记录偏移与内联参数值,miss 分支收集候选方法。返回 err 表示
// 路径非法。segStart 是当前段的起始偏移:radix 碎片层消费同一段,段偏移记录在
// 完整段上;每个分支尝试前成对回溯段表与值表(Truncate),保证最终只保留成功
// 路径的记录。
// lookupAllow scans raw[pos:]: static segments compare whole with memcmp via
// the first-byte table, param segments bound in one scan that also validates,
// records the offset and appends the inline param value, and missed branches
// collect candidate methods. A non-nil err marks an invalid path. segStart
// tracks the current segment's start so radix fragments record whole-segment
// offsets; each branch rewinds the segment table and the value table in pairs
// before trying, so only the successful path's records survive.
func (n *routeMuxNode) lookupAllow(raw string, pos int, segStart int, w *routeWalkState) (*compiledRoute, error) {
	segments := w.segments
	paramValues := w.params
	segMark := segments.Len()
	paramMark := paramValues.Len()
	if pos >= len(raw) {
		trailing := w.strict && len(raw) > 0 && raw[len(raw)-1] == '/'
		set := n.terminal(trailing)
		if route := set.find(w.method); route != nil {
			return route, nil
		}
		w.allow.addSet(set)
		if n.catchAll != nil {
			set := n.catchAll.terminal(trailing)
			if route := set.find(w.method); route != nil {
				return route, nil
			}
			w.allow.addSet(set)
		}
		return nil, nil
	}

	// 静态子节点:整段 memcmp(首字节分派);失败后若本段含转义再做解码比较。
	// static children: whole-segment memcmp via the first-byte table; on
	// failure, decoded comparison runs only when the segment contains escapes.
	if n.idx != nil {
		if ci := n.idx[raw[pos]]; ci >= 0 {
			if route, matched, err := n.matchStaticChild(n.children[ci], raw, pos, segStart, w); matched || err != nil {
				return route, err
			}
		}
	} else {
		for i := range n.children {
			segments.Truncate(segMark)
			paramValues.Truncate(paramMark)
			if route, matched, err := n.matchStaticChild(n.children[i], raw, pos, segStart, w); matched || err != nil {
				return route, err
			}
		}
	}

	// 单遍段扫描:段尾与 '%' 同一次遍历得出,转义兜底与参数分支共用;
	// 仅 catchAll 的层跳过(它自扫剩余段)。
	// one segment scan yields both the segment end and '%': the escape
	// fallback and the param branch share it; catchAll-only layers skip it
	// because they scan the remainder themselves.
	if n.parameter != nil || len(n.children) > 0 {
		end, hasEscape := segmentEndEscape(raw, segStart)
		if len(n.children) > 0 && hasEscape {
			for i := range n.children {
				segments.Truncate(segMark)
				paramValues.Truncate(paramMark)
				child := n.children[i]
				if !staticSegmentMatches(raw[segStart:end], child.segment) {
					continue
				}
				next := end
				if next < len(raw) {
					next++
				}
				segments.Add(pathSegment{start: segStart, end: end})
				route, err := child.node.lookupAllow(raw, next, next, w)
				if err != nil {
					return nil, err
				}
				if route != nil {
					return route, nil
				}
			}
		}

		// 参数子节点:校验、记录偏移并内联追加参数值(按槽位序)。参数段从段首
		// 消费,pos 与 segStart 必然一致(参数子节点只挂在段尾层)。
		// param child: validate, record the offset and append the inline
		// param value in slot order. The param consumes the whole segment, so
		// pos always equals segStart here (param children only hang off
		// segment-boundary nodes).
		if n.parameter != nil {
			if len(n.children) > 0 {
				segments.Truncate(segMark)
				paramValues.Truncate(paramMark)
			}
			segment := raw[segStart:end]
			if err := validateRawSegmentChecked(segment, hasEscape); err != nil {
				return nil, err
			}
			segments.Add(pathSegment{start: segStart, end: end})
			// 内联参数值:无 '%' 零拷贝切片;转义段记录 escaped,解码推迟到
			// 命中后的命名映射(与 fillMatchedParams 的错误语义一致)。
			// inline param value: a zero-copy slice without '%'; escaped
			// segments record the flag and defer decoding to the post-hit
			// named mapping (keeping fillMatchedParams error semantics).
			paramValues.Add(walkParamValue{value: segment, escaped: hasEscape})
			next := end
			if next < len(raw) {
				next++
			}
			route, err := n.parameter.lookupAllow(raw, next, next, w)
			if err != nil {
				return nil, err
			}
			if route != nil {
				return route, nil
			}
		}
	}

	// catch-all 子节点:剩余段逐个校验并记录(单遍扫描),拼出整体值后查终端
	// 方法。拼接与 rawJoinFromSegments 一致:自首个剩余段起点到最后记录段尾。
	// catch-all child: validate and record each remaining segment (single
	// scan) and look up the terminal method. The joined value matches
	// rawJoinFromSegments: from the first remaining segment's start to the
	// last recorded segment's end.
	if n.catchAll != nil {
		segments.Truncate(segMark)
		paramValues.Truncate(paramMark)
		lastEnd := segStart
		catchAllEscape := false
		for p := segStart; p < len(raw); {
			nextEnd, escaped := segmentEndEscape(raw, p)
			if err := validateRawSegmentChecked(raw[p:nextEnd], escaped); err != nil {
				return nil, err
			}
			segments.Add(pathSegment{start: p, end: nextEnd})
			lastEnd = nextEnd
			catchAllEscape = catchAllEscape || escaped
			if nextEnd >= len(raw) {
				break
			}
			p = nextEnd + 1
		}
		paramValues.Add(walkParamValue{value: raw[segStart:lastEnd], escaped: catchAllEscape})
		trailing := w.strict && len(raw) > 0 && raw[len(raw)-1] == '/'
		set := n.catchAll.terminal(trailing)
		if route := set.find(w.method); route != nil {
			return route, nil
		}
		w.allow.addSet(set)
	}
	segments.Truncate(segMark)
	paramValues.Truncate(paramMark)
	return nil, nil
}

// matchStaticChild 尝试一个静态子节点:整段 memcmp,段尾消费时记录整段偏移。
// matchStaticChild tries one static child with a whole-segment memcmp and
// records the whole segment when the boundary is consumed.
func (n *routeMuxNode) matchStaticChild(child routeStaticChild, raw string, pos int, segStart int, w *routeWalkState) (*compiledRoute, bool, error) {
	lp := len(child.segment)
	if pos+lp > len(raw) || raw[pos:pos+lp] != child.segment {
		return nil, false, nil
	}
	next := pos + lp
	if next >= len(raw) || raw[next] == '/' {
		// 完整段消费(段尾 '/' 或路径末尾):记录整段偏移后进入下一段。
		// whole segment consumed (boundary '/' or end of path): record it
		// and move to the next segment.
		w.segments.Add(pathSegment{start: segStart, end: next})
		if next < len(raw) {
			next++
		}
		// 链式折叠:整段消费后,若后续节点是"纯参数节点"(只挂一个参数)或
		// "单静态子节点"(只挂一个静态子),就地循环消费整条链——参数段扫描/
		// 校验/偏移/内联值,静态段 memcmp——直到遇到非平凡节点(多路分支/
		// 终端路由/catch-all/参数与静态并存)交给 lookupAllow,或链死(路径
		// 耗尽或段不匹配)。等价性:链上每层在原递归里只有一条可走分支;段
		// 不匹配时回退该层 lookupAllow 重放,覆盖转义兜底与段内碎片等罕见
		// 分支;链死时上层分支的成对 Truncate 照常回退已消费记录。
		// chain collapse: after a whole-segment consumption, when the
		// following nodes are param-only (a single parameter child) or
		// single-static (a single static child), consume the whole chain
		// inline — param segments scanned, validated and recorded with their
		// inline values, static segments memcmp'd — until a non-trivial node
		// (multi-way branch, terminal routes, catch-all, or a node mixing
		// params with static children) hands off to lookupAllow, or the chain
		// dies (path exhausted or segment mismatch). Equivalence: every chain
		// level had exactly one viable branch in the original recursion; a
		// segment mismatch rewinds to that level's lookupAllow replay, which
		// covers the escape fallback and in-segment fragments; a dead chain
		// unwinds its consumed records through the caller branches' paired
		// Truncate as before.
		node := child.node
		for {
			if node.parameter != nil && len(node.children) == 0 && node.catchAll == nil && len(node.routes.routes) == 0 && len(node.trailing.routes) == 0 {
				// 纯参数节点:原递归在此只会走参数分支。
				// param-only node: the original recursion would only take
				// the param branch here.
				if next >= len(raw) {
					return nil, false, nil
				}
				end, hasEscape := segmentEndEscape(raw, next)
				segment := raw[next:end]
				if err := validateRawSegmentChecked(segment, hasEscape); err != nil {
					return nil, true, err
				}
				w.segments.Add(pathSegment{start: next, end: end})
				w.params.Add(walkParamValue{value: segment, escaped: hasEscape})
				node = node.parameter
				next = end
				if next < len(raw) {
					next++
				}
				continue
			}
			if node.parameter == nil && node.catchAll == nil && len(node.routes.routes) == 0 && len(node.trailing.routes) == 0 && len(node.children) == 1 {
				// 单静态子节点:整段 memcmp 成功且段尾边界成立才推进;
				// 否则回退本层慢路径(重放转义兜底/段内碎片)。
				// single-static node: advance only on a whole-segment
				// memcmp with a boundary; otherwise rewind into this
				// level's slow path (replaying the escape fallback and
				// in-segment fragments).
				only := node.children[0]
				lp := len(only.segment)
				if next+lp > len(raw) || raw[next:next+lp] != only.segment || (next+lp < len(raw) && raw[next+lp] != '/') {
					route, err := node.lookupAllow(raw, next, next, w)
					if err != nil {
						return nil, true, err
					}
					if route != nil {
						return route, true, nil
					}
					return nil, false, nil
				}
				w.segments.Add(pathSegment{start: next, end: next + lp})
				node = only.node
				next += lp
				if next < len(raw) {
					next++
				}
				continue
			}
			// 非平凡节点:交回慢路径(静态分支/参数并存/终端/allow 收集)。
			// non-trivial node: hand back to the slow path (static
			// branching, mixed param+static, terminals, allow collection).
			route, err := node.lookupAllow(raw, next, next, w)
			if err != nil {
				return nil, true, err
			}
			if route != nil {
				return route, true, nil
			}
			return nil, false, nil
		}
	}
	// 段内碎片:继续深入同段,segStart 不变。
	// in-segment fragment: keep descending within the same segment.
	route, err := child.node.lookupAllow(raw, next, segStart, w)
	if err != nil {
		return nil, true, err
	}
	if route != nil {
		return route, true, nil
	}
	return nil, false, nil
}

// segmentEndEscape 单遍扫描返回段尾偏移与段内是否含 '%':短段(REST 常态)
// 一次字节循环同时完成定界与转义探测,替代 segmentEnd + IndexByte 两次调用。
// segmentEndEscape returns the segment end offset and whether it contains
// '%' in one pass: a single byte loop over short segments (the REST norm)
// does both the bounding and the escape probe, replacing the two separate
// segmentEnd and IndexByte calls.
func segmentEndEscape(raw string, pos int) (int, bool) {
	hasEscape := false
	for i := pos; i < len(raw); i++ {
		switch raw[i] {
		case '/':
			return i, hasEscape
		case '%':
			hasEscape = true
		}
	}
	return len(raw), hasEscape
}

// validateRawSegmentChecked 是 validateRawSegmentQuick 的转义预知变体:调用方
// 已在单遍扫描中得出 hasEscape,免去重复的 '%' 扫描。
// validateRawSegmentChecked is the escape-aware variant of
// validateRawSegmentQuick: the caller already derived hasEscape in the
// single scan, skipping the redundant '%' pass.
func validateRawSegmentChecked(raw string, hasEscape bool) error {
	if raw == "" {
		return fmt.Errorf("%w: path contains an empty segment", ErrInvalidRequestPath)
	}
	if hasEscape {
		return validateRawSegment(raw)
	}
	if len(raw) <= 2 && (raw == "." || raw == "..") {
		return fmt.Errorf("%w: %q contains dot segment", ErrInvalidRequestPath, raw)
	}
	return nil
}

// segmentEnd 返回 raw 中 pos 所在段的末尾偏移(不含段尾 '/')。
// segmentEnd returns the end offset of the segment containing pos.
func segmentEnd(raw string, pos int) int {
	if idx := strings.IndexByte(raw[pos:], '/'); idx >= 0 {
		return pos + idx
	}
	return len(raw)
}

// validateRawSegmentQuick 快速校验一段:含 '%' 走完整校验,无转义段只做
// dot 段检查(长度 ≤2)。空段由调用方保证非空时直接返回 nil。
// validateRawSegmentQuick validates one segment: '%' triggers the full check,
// unescaped segments only need the dot check for short segments.
func validateRawSegmentQuick(raw string) error {
	if raw == "" {
		return fmt.Errorf("%w: path contains an empty segment", ErrInvalidRequestPath)
	}
	if strings.IndexByte(raw, '%') >= 0 {
		return validateRawSegment(raw)
	}
	if len(raw) <= 2 && (raw == "." || raw == "..") {
		return fmt.Errorf("%w: %q contains dot segment", ErrInvalidRequestPath, raw)
	}
	return nil
}

// validateRawPath 逐段校验整条路径(零分配),供 miss 兜底区分 400/404。
// validateRawPath validates every segment of the path without allocating; it
// backs the miss path's 400-vs-404 decision.
func validateRawPath(rawPath string) error {
	if rawPath == "" || rawPath == "/" {
		return nil
	}
	path := rawPath
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	body := path[1:]
	if strings.HasSuffix(body, "/") {
		body = body[:len(body)-1]
	}
	for start := 0; start < len(body); {
		end := segmentEnd(body, start)
		if err := validateRawSegmentQuick(body[start:end]); err != nil {
			return err
		}
		if end == len(body) {
			break
		}
		start = end + 1
	}
	return nil
}

// commonPrefix 返回两个字符串的公共前缀长度。
// commonPrefix returns the shared prefix length of two strings.
func commonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	i := 0
	for i < max && a[i] == b[i] {
		i++
	}
	return i
}

// staticSegmentMatches 判断原始段是否命中注册文本:直接相等,或段含转义时
// 解码后逐字节比较。
// staticSegmentMatches reports whether a raw segment hits the registered text:
// direct equality, or decoded byte-wise comparison when the raw contains escapes.
func staticSegmentMatches(raw, expected string) bool {
	if raw == expected {
		return true
	}
	if strings.IndexByte(raw, '%') < 0 {
		return false
	}
	return rawSegmentMatches(raw, expected)
}

func (n *routeMuxNode) terminal(trailing bool) *routeMethodSet {
	if trailing {
		return &n.trailing
	}
	return &n.routes
}

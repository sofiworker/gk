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
}

type compiledRoute struct {
	definition routeDefinition
	handler    http.Handler
	// fastDirect 是无状态直调:无中间件、无错误模型且请求无 body、非 HEAD 时
	// 执行,params 由匹配结果预提取传入。nil 表示该路由必须走状态注入路径。
	// fastDirect is the stateless direct call: executed when there is no
	// middleware or error model and the request is body-less and not HEAD;
	// params arrive pre-extracted from the match. nil forces the state path.
	fastDirect func(http.ResponseWriter, *http.Request, pathParamList)
	// directName 记录"静态前缀 + 单末尾参数"直达索引的参数名。
	// directName holds the param name of the direct single-trailing-param index.
	directName string
	// needsParams 标记 fastDirect 消费预提取的参数(stateIndependent 直调形态)。
	// needsParams marks fastDirect as consuming pre-extracted params (the
	// stateIndependent direct shape).
	needsParams bool
	// paramPos 记录参数名到模式段索引的映射,供惰性解码按 key 定位。
	// paramPos maps a param name to its pattern segment index for lazy decoding.
	paramPos map[string]int
}

// extractMatched 解码已成功匹配路由的动态段，不重复校验静态结构。
// extractMatched decodes dynamic segments of an already matched route without
// revalidating its static structure.
func (r *compiledRoute) extractMatched(path requestPath) (pathParamList, error) {
	var params pathParamList
	for index, segment := range r.definition.pattern.segments {
		switch segment.kind {
		case routeSegmentParameter:
			value, err := path.DecodeAt(index)
			if err != nil {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, r.definition.pattern.path)
			}
			params.Add(segment.value, value)
		case routeSegmentCatchAll:
			value, err := url.PathUnescape(path.RawJoinFrom(index))
			if err != nil {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, r.definition.pattern.path)
			}
			params.Add(segment.value, value)
		}
	}
	return params, nil
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
			if segment.kind == routeSegmentParameter || segment.kind == routeSegmentCatchAll {
				route.paramPos[segment.value] = index
			}
		}
		mux.routes = append(mux.routes, route)
		mux.root.insert(definition.pattern.segments, route)
		if !definition.pattern.hasParams() {
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
	return mux
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
	byPath := m.staticByMethod[method]
	if byPath == nil && method != http.MethodHead {
		return routeMatchResult{}, false
	}
	if method == http.MethodHead && byPath == nil &&
		m.staticByMethod[http.MethodGet] == nil {
		return routeMatchResult{}, false
	}
	if strings.IndexByte(path, '%') >= 0 {
		return routeMatchResult{}, false
	}
	if method == http.MethodGet {
		if route := m.staticGet[path]; route != nil {
			return routeMatchResult{kind: routeMatchFound, route: route}, true
		}
		return routeMatchResult{}, false
	}
	if route := byPath[path]; route != nil {
		return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: method == http.MethodHead}, true
	}
	if method == http.MethodHead {
		if byPath := m.staticByMethod[http.MethodHead]; byPath != nil {
			if route := byPath[path]; route != nil {
				return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: true}, true
			}
		}
		if byPath := m.staticByMethod[http.MethodGet]; byPath != nil {
			if route := byPath[path]; route != nil {
				return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: true}, true
			}
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
	byPrefix[prefix.String()] = route
}

// matchDirectParam 匹配未转义的简单单参数路径；其他形态返回 false 走完整树。
// matchDirectParam matches an unescaped simple single-parameter path; all other
// shapes return false and fall back to the full tree.
func (m *routeMux) matchDirectParam(method, path string) (*compiledRoute, string, bool) {
	if m == nil || m.directParamByMethod == nil || method == http.MethodHead || path == "" {
		return nil, "", false
	}
	byPrefix := m.directParamByMethod[method]
	if byPrefix == nil {
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
	route := byPrefix[path[:slash+1]]
	return route, value, route != nil
}

// match 扫描原始请求路径:radix 树一次遍历完成匹配、段偏移收集与 405 方法
// 收集。路径非法(空段/非法转义/dot 段)返回 routeMatchBadPath。
// match scans the raw request path: one radix traversal matches, collects
// segment offsets and gathers 405 methods. Invalid paths (empty segments,
// bad escapes, dot segments) return routeMatchBadPath.
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
	lookup := func(candidate string) (*compiledRoute, error) {
		return m.root.lookupAllow(candidate, scanPath, 0, 0, strict, &allow, &segments)
	}

	var route *compiledRoute
	var err error
	suppressBody := method == http.MethodHead
	if method == http.MethodHead {
		if route, err = lookup(http.MethodHead); route == nil && err == nil {
			route, err = lookup(http.MethodGet)
		}
	} else {
		route, err = lookup(method)
	}
	if err != nil {
		return routeMatchResult{kind: routeMatchBadPath, badPath: err}
	}
	if route != nil {
		return routeMatchResult{
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

// lookupAllow 扫描 raw[pos:]:静态段整段 memcmp(首字节分派),参数段 IndexByte
// 定界并顺带校验与记录偏移,miss 分支收集候选方法。返回 err 表示路径非法。
// segStart 是当前段的起始偏移:radix 碎片层消费同一段,段偏移记录在完整段上;
// 每个分支尝试前回溯段列表(Truncate),保证最终只保留成功路径的段。
// lookupAllow scans raw[pos:]: static segments compare whole with memcmp via
// the first-byte table, param segments bound via IndexByte while validating
// and recording offsets, and missed branches collect candidate methods. A
// non-nil err marks an invalid path. segStart tracks the current segment's
// start so radix fragments record whole-segment offsets; each branch
// backtracks the segment list first, so only the successful path survives.
func (n *routeMuxNode) lookupAllow(method string, raw string, pos int, segStart int, strict bool, allow *methodCollector, segments *pathSegmentList) (*compiledRoute, error) {
	mark := segments.Len()
	if pos >= len(raw) {
		trailing := strict && len(raw) > 0 && raw[len(raw)-1] == '/'
		set := n.terminal(trailing)
		if route := set.find(method); route != nil {
			return route, nil
		}
		allow.addSet(set)
		if n.catchAll != nil {
			set := n.catchAll.terminal(trailing)
			if route := set.find(method); route != nil {
				return route, nil
			}
			allow.addSet(set)
		}
		return nil, nil
	}

	// 静态子节点:整段 memcmp(首字节分派);失败后若本段含转义再做解码比较。
	// static children: whole-segment memcmp via the first-byte table; on
	// failure, decoded comparison runs only when the segment contains escapes.
	if n.idx != nil {
		if ci := n.idx[raw[pos]]; ci >= 0 {
			if route, matched, err := n.matchStaticChild(n.children[ci], method, raw, pos, segStart, strict, allow, segments); matched || err != nil {
				return route, err
			}
		}
	} else {
		for i := range n.children {
			segments.Truncate(mark)
			if route, matched, err := n.matchStaticChild(n.children[i], method, raw, pos, segStart, strict, allow, segments); matched || err != nil {
				return route, err
			}
		}
	}

	// 转义兜底与参数分支共享一次段尾计算;无静态子节点的层跳过 % 检查。
	// the escape fallback and the param branch share one segment-end lookup;
	// layers without static children skip the '%' scan entirely.
	end := segmentEnd(raw, segStart)
	if len(n.children) > 0 && strings.IndexByte(raw[segStart:end], '%') >= 0 {
		for i := range n.children {
			segments.Truncate(mark)
			child := n.children[i]
			if !staticSegmentMatches(raw[segStart:end], child.segment) {
				continue
			}
			next := end
			if next < len(raw) {
				next++
			}
			segments.Add(pathSegment{start: segStart, end: end})
			route, err := child.node.lookupAllow(method, raw, next, next, strict, allow, segments)
			if err != nil {
				return nil, err
			}
			if route != nil {
				return route, nil
			}
		}
	}

	// 参数子节点:校验并记录偏移。参数段从段首消费,pos 与 segStart 必然一致
	// (参数子节点只挂在段尾层)。仅当本层尝试过静态子时才需要回溯段列表。
	// param child: validate and record the offset. The param consumes the
	// whole segment, so pos always equals segStart here (param children only
	// hang off segment-boundary nodes). Only layers that tried static children
	// need to rewind the segment list.
	if n.parameter != nil {
		if len(n.children) > 0 {
			segments.Truncate(mark)
		}
		if err := validateRawSegmentQuick(raw[segStart:end]); err != nil {
			return nil, err
		}
		segments.Add(pathSegment{start: segStart, end: end})
		next := end
		if next < len(raw) {
			next++
		}
		route, err := n.parameter.lookupAllow(method, raw, next, next, strict, allow, segments)
		if err != nil {
			return nil, err
		}
		if route != nil {
			return route, nil
		}
	}

	// catch-all 子节点:剩余段逐个校验并记录,再查终端方法。
	// catch-all child: validate and record the remaining segments, then look
	// up the terminal method.
	if n.catchAll != nil {
		segments.Truncate(mark)
		segEnd := segStart
		for segEnd < len(raw) {
			nextEnd := segmentEnd(raw, segEnd)
			if err := validateRawSegmentQuick(raw[segEnd:nextEnd]); err != nil {
				return nil, err
			}
			segments.Add(pathSegment{start: segEnd, end: nextEnd})
			if nextEnd >= len(raw) {
				segEnd = nextEnd
				break
			}
			segEnd = nextEnd + 1
		}
		trailing := strict && len(raw) > 0 && raw[len(raw)-1] == '/'
		set := n.catchAll.terminal(trailing)
		if route := set.find(method); route != nil {
			return route, nil
		}
		allow.addSet(set)
	}
	segments.Truncate(mark)
	return nil, nil
}

// matchStaticChild 尝试一个静态子节点:整段 memcmp,段尾消费时记录整段偏移。
// matchStaticChild tries one static child with a whole-segment memcmp and
// records the whole segment when the boundary is consumed.
func (n *routeMuxNode) matchStaticChild(child routeStaticChild, method string, raw string, pos int, segStart int, strict bool, allow *methodCollector, segments *pathSegmentList) (*compiledRoute, bool, error) {
	lp := len(child.segment)
	if pos+lp > len(raw) || raw[pos:pos+lp] != child.segment {
		return nil, false, nil
	}
	next := pos + lp
	if next >= len(raw) || raw[next] == '/' {
		// 完整段消费(段尾 '/' 或路径末尾):记录整段偏移后进入下一段。
		// whole segment consumed (boundary '/' or end of path): record it
		// and move to the next segment.
		segments.Add(pathSegment{start: segStart, end: next})
		if next < len(raw) {
			next++
		}
		route, err := child.node.lookupAllow(method, raw, next, next, strict, allow, segments)
		if err != nil {
			return nil, true, err
		}
		if route != nil {
			return route, true, nil
		}
		return nil, false, nil
	}
	// 段内碎片:继续深入同段,segStart 不变。
	// in-segment fragment: keep descending within the same segment.
	route, err := child.node.lookupAllow(method, raw, next, segStart, strict, allow, segments)
	if err != nil {
		return nil, true, err
	}
	if route != nil {
		return route, true, nil
	}
	return nil, false, nil
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

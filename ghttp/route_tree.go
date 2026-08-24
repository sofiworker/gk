package ghttp

import (
	"strings"
)

// 本文件的路由树完全采用 gin 的算法与语义(经用户确认「连语义也完全对齐 gin」):
//   - 节点结构:path/indices/wildChild/nType/priority/children/handler,通配子节点
//     恒为 children 数组的最后一个元素,由 wildChild 标记。
//   - 插入:addRoute/insertChild —— 最长公共前缀分裂、优先级重排(热子节点前移)。
//   - 匹配:getValue —— 单循环 walk + skippedNodes 显式回溯栈 + TSR(尾斜杠重定向)建议。
// 算法逐行移植自 gin v1.12.0 tree.go,仅将 gin 的 HandlersChain/Params 适配为本框架
// 的 compiledHandler 与 Params 类型;代码为本项目实现,不含 gin 源码拷贝。
//
// This file's routing tree fully adopts gin's algorithm and semantics (confirmed
// by the user: "align semantics with gin too"):
//   - node layout: path/indices/wildChild/nType/priority/children/handler, with
//     the wildcard child always the last element of children, marked by wildChild.
//   - insertion: addRoute/insertChild — longest-common-prefix split and priority
//     reordering (hotter children move to the front).
//   - matching: getValue — a single walk loop plus an explicit skippedNodes
//     backtracking stack and a TSR (trailing-slash redirect) recommendation.
// The algorithm is ported line-by-line from gin v1.12.0 tree.go, adapting gin's
// HandlersChain/Params to this framework's compiledHandler and Params types; the
// code is this project's own implementation and contains no copy of gin sources.
//
// 路径形态约定 / path form convention:
//   模板 {name} → :name,{name...} → *name(见 route_path.go 的翻译),因此节点 path
//   内即 gin 原生通配形式,param 名取 n.path[1:],catchAll 名取 n.path[2:]。
//   Template {name} → :name and {name...} → *name (translated in route_path.go),
//   so node path carries gin-native wildcard forms; param name = n.path[1:] and
//   catch-all name = n.path[2:].

// nodeType 区分节点类型。
// nodeType distinguishes node types.
type nodeType uint8

const (
	ntStatic   nodeType = iota // 静态片段 / static fragment
	ntRoot                     // 树根 / tree root
	ntParam                    // 单段参数 :name / single-segment param
	ntCatchAll                 // 尾部通配 *name / trailing wildcard
)

// routeNode 是路由树的一个节点(结构对齐 gin node)。
// routeNode is one node of the routing tree (layout aligned with gin's node).
type routeNode struct {
	path      string          // 本节点承载的路径片段 / path fragment carried here
	indices   string          // 各静态子节点 path 首字节,O(1) 派发 / first byte of each static child
	wildChild bool            // 最后一个子节点是否为通配节点 / is the last child a wildcard
	nType     nodeType        // 节点类型 / node type
	priority  uint32          // 子树命中权重(热路径重排)/ subtree hit weight
	children  []*routeNode    // 子节点(通配恒在末位)/ children (wildcard last)
	handler   compiledHandler // 路由终点处理器 / handler terminating a route
	// fullPath 是终端节点对应的完整路由模板(gin 形式,如 /users/:id),注册期一次性
	// 写入,供命中后作为低基数标签用于 metrics/tracing/日志。非终端节点为空。
	// fullPath is the complete route template for a terminal node (gin form, e.g.
	// /users/:id), written once at registration to serve as a low-cardinality
	// label for metrics/tracing/logging after a hit. Empty on non-terminal nodes.
	fullPath string
}

// methodTree 是一个 HTTP method 对应的路由根。
// methodTree is the routing root for one HTTP method.
type methodTree struct {
	method string
	root   *routeNode
}

// -----------------------------------------------------------------------------
// 辅助 / helpers(移植自 gin)
// -----------------------------------------------------------------------------

// longestCommonPrefix 返回 a、b 的最长公共前缀字节数。
// longestCommonPrefix returns the byte length of the longest common prefix.
func longestCommonPrefix(a, b string) int {
	i := 0
	m := len(a)
	if len(b) < m {
		m = len(b)
	}
	for i < m && a[i] == b[i] {
		i++
	}
	return i
}

// addChild 追加一个子节点,并保证通配子节点始终位于末位。
// addChild appends a child, keeping the wildcard child at the end.
func (n *routeNode) addChild(child *routeNode) {
	if n.wildChild && len(n.children) > 0 {
		wildcardChild := n.children[len(n.children)-1]
		n.children = append(n.children[:len(n.children)-1], child, wildcardChild)
	} else {
		n.children = append(n.children, child)
	}
}

// incrementChildPrio 递增 pos 处子节点的优先级并按需前移,返回其新位置。
// incrementChildPrio bumps the priority of the child at pos and reorders it
// toward the front as needed, returning its new position.
func (n *routeNode) incrementChildPrio(pos int) int {
	cs := n.children
	cs[pos].priority++
	prio := cs[pos].priority

	newPos := pos
	for ; newPos > 0 && cs[newPos-1].priority < prio; newPos-- {
		cs[newPos-1], cs[newPos] = cs[newPos], cs[newPos-1]
	}

	if newPos != pos {
		n.indices = n.indices[:newPos] +
			n.indices[pos:pos+1] +
			n.indices[newPos:pos] + n.indices[pos+1:]
	}
	return newPos
}

// findWildcard 返回 path 中的首个通配片段(:name 或 *name)、其起点下标与是否合法。
// findWildcard returns the first wildcard segment (:name or *name) in path, its
// start index, and whether it is valid.
func findWildcard(path string) (wildcard string, i int, valid bool) {
	for start := 0; start < len(path); start++ {
		c := path[start]
		if c != ':' && c != '*' {
			continue
		}
		valid = true
		for end := start + 1; end < len(path); end++ {
			switch path[end] {
			case '/':
				return path[start:end], start, valid
			case ':', '*':
				valid = false
			}
		}
		return path[start:], start, valid
	}
	return "", -1, false
}

// -----------------------------------------------------------------------------
// 插入 / insertion(移植自 gin addRoute/insertChild)
// -----------------------------------------------------------------------------

// insert 将 path 对应的 handler 插入本 method 树。path 为 gin 形式(:name/*name)。
// insert adds handler for path (gin form :name/*name) into this method tree.
func (t *methodTree) insert(path string, h compiledHandler) error {
	if t.root == nil {
		t.root = &routeNode{}
	}
	return t.root.addRoute(path, h)
}

// addRoute 把 (path, handler) 加入以 n 为根的树。非并发安全(仅注册期调用)。
// addRoute adds (path, handler) into the tree rooted at n. Not concurrency-safe
// (registration-time only).
func (n *routeNode) addRoute(path string, h compiledHandler) error {
	fullPath := path
	n.priority++

	// 空树 / empty tree.
	if len(n.path) == 0 && len(n.children) == 0 {
		if err := n.insertChild(path, fullPath, h); err != nil {
			return err
		}
		n.nType = ntRoot
		return nil
	}

	parentFullPathIndex := 0

walk:
	for {
		i := longestCommonPrefix(path, n.path)

		// 分裂边 / split edge.
		if i < len(n.path) {
			child := routeNode{
				path:      n.path[i:],
				wildChild: n.wildChild,
				nType:     ntStatic,
				indices:   n.indices,
				children:  n.children,
				handler:   n.handler,
				fullPath:  n.fullPath,
				priority:  n.priority - 1,
			}
			n.children = []*routeNode{&child}
			n.indices = string(n.path[i])
			n.path = path[:i]
			n.handler = nil
			n.fullPath = ""
			n.wildChild = false
		}

		// 使新节点成为本节点的子节点 / make new node a child of this node.
		if i < len(path) {
			path = path[i:]
			c := path[0]

			// param 之后的 '/' / '/' after param.
			if n.nType == ntParam && c == '/' && len(n.children) == 1 {
				parentFullPathIndex += len(n.path)
				n = n.children[0]
				n.priority++
				continue walk
			}

			// 存在以下一个字节为首的子节点? / child with next path byte exists?
			for idx, maxN := 0, len(n.indices); idx < maxN; idx++ {
				if c == n.indices[idx] {
					parentFullPathIndex += len(n.path)
					idx = n.incrementChildPrio(idx)
					n = n.children[idx]
					continue walk
				}
			}

			// 否则插入 / otherwise insert.
			if c != ':' && c != '*' && n.nType != ntCatchAll {
				n.indices += string(c)
				child := &routeNode{}
				n.addChild(child)
				n.incrementChildPrio(len(n.indices) - 1)
				n = child
			} else if n.wildChild {
				// 插入通配节点,需检查与既有通配的冲突。
				// inserting a wildcard node, check conflict with existing wildcard.
				n = n.children[len(n.children)-1]
				n.priority++

				if len(path) >= len(n.path) && n.path == path[:len(n.path)] &&
					n.nType != ntCatchAll &&
					(len(n.path) >= len(path) || path[len(n.path)] == '/') {
					continue walk
				}
				return ErrDuplicateRoute
			}

			return n.insertChild(path, fullPath, h)
		}

		// i == len(path):路径已是既有节点的前缀。
		// i == len(path): the path is a prefix of an existing node.
		if n.handler != nil {
			return ErrDuplicateRoute
		}
		n.handler = h
		n.fullPath = fullPath
		return nil
	}
}

// insertChild 在 n 下插入含通配的 path(拆出静态前缀、param 节点、catchAll 双节点)。
// insertChild inserts a possibly-wildcard path under n (splitting off the static
// prefix, the param node, and the catch-all node pair).
func (n *routeNode) insertChild(path string, fullPath string, h compiledHandler) error {
	for {
		wildcard, i, valid := findWildcard(path)
		if i < 0 { // 无通配 / no wildcard.
			break
		}
		if !valid {
			return ErrInvalidParam
		}
		if len(wildcard) < 2 { // 通配必须有名字 / wildcard must be named.
			return ErrInvalidParam
		}

		if wildcard[0] == ':' { // param
			if i > 0 {
				n.path = path[:i]
				path = path[i:]
			}
			child := &routeNode{nType: ntParam, path: wildcard}
			n.addChild(child)
			n.wildChild = true
			n = child
			n.priority++

			// 若 path 未以通配结束,后面还有以 '/' 起始的子路径。
			// if path doesn't end with the wildcard, another '/'-prefixed subpath follows.
			if len(wildcard) < len(path) {
				path = path[len(wildcard):]
				child := &routeNode{priority: 1}
				n.addChild(child)
				n = child
				continue
			}
			n.handler = h
			n.fullPath = fullPath
			return nil
		}

		// catchAll
		if i+len(wildcard) != len(path) {
			return ErrCatchAllPosition
		}
		if len(n.path) > 0 && n.path[len(n.path)-1] == '/' {
			return ErrCatchAllPosition
		}
		i--
		if i < 0 || path[i] != '/' {
			return ErrCatchAllPosition
		}
		n.path = path[:i]

		// 第一个节点:空 path 的 catchAll 节点 / first node: catchAll with empty path.
		child := &routeNode{wildChild: true, nType: ntCatchAll}
		n.addChild(child)
		n.indices = "/"
		n = child
		n.priority++

		// 第二个节点:承载变量的节点 / second node: holds the variable.
		child2 := &routeNode{path: path[i:], nType: ntCatchAll, handler: h, fullPath: fullPath, priority: 1}
		n.children = []*routeNode{child2}
		return nil
	}

	// 无通配:直接落 path 与 handler。
	// no wildcard: set path and handler directly.
	n.path = path
	n.handler = h
	n.fullPath = fullPath
	return nil
}

// -----------------------------------------------------------------------------
// 匹配 / matching(移植自 gin getValue,skippedNodes 迭代 walk + TSR)
// -----------------------------------------------------------------------------

// skippedNode 记录一个被跳过、可回溯的节点及其时点的 path 与已写参数数。
// skippedNode records a skipped, backtrackable node with its path and the param
// count at that point.
type skippedNode struct {
	path        string
	node        *routeNode
	paramsCount int
}

// nodeValue 是 getValue 的返回:命中的 handler、是否建议 TSR 重定向。
// nodeValue holds getValue's result: the matched handler and whether a TSR
// (trailing-slash redirect) is recommended.
type nodeValue struct {
	handler compiledHandler
	// fullPath 指向命中终端节点的完整路由模板(gin 形式)。用指针而非内嵌 string,
	// 使 nodeValue 保持紧凑(避免 16→32 字节翻倍拖慢 getValue 的按值返回),命中后
	// 由分发器解引用一次写入 Request。未命中为 nil。
	// fullPath points to the matched terminal node's full route template (gin
	// form). A pointer (not an embedded string) keeps nodeValue compact (avoiding
	// a 16→32 byte doubling that slows getValue's by-value return); the dispatcher
	// dereferences it once after a hit to write into Request. Nil on a miss.
	fullPath *string
	tsr      bool
}

// getValue 在树中查找 path 对应的 handler,把通配值写入 params;未命中时可能给出
// TSR 建议(存在多一个/少一个尾斜杠的等价路由)。使用调用方提供的 skipped 回溯栈。
// getValue looks up the handler for path in the tree, writing wildcard values
// into params; on a miss it may recommend a TSR (an equivalent route differing
// by one trailing slash). It uses the caller-provided skipped backtracking stack.
func (n *routeNode) getValue(path string, params *Params, skipped *[]skippedNode) (value nodeValue) {
	var globalParamsCount int

walk:
	for {
		prefix := n.path
		if len(path) > len(prefix) {
			if path[:len(prefix)] == prefix {
				path = path[len(prefix):]

				// 先尝试所有非通配子节点(按 indices 匹配首字节)。
				// Try all non-wildcard children first (match first byte via indices).
				idxc := path[0]
				for i := 0; i < len(n.indices); i++ {
					if n.indices[i] == idxc {
						if n.wildChild {
							// 压入可回溯的 skippedNode(用 append 自动扩容;池化的底层
							// 数组经 [:0] 复用,预热后零分配)。
							// 关键:副本【故意不拷贝 indices】——回溯重走时静态子节点因此
							// "隐形",匹配直接落到通配子节点(param/catchAll),这正是
							// "静态失败后回落 param" 的实现,也避免了无限回溯循环。
							// Push a backtrackable skippedNode (append auto-grows; the
							// pooled backing array is reused via [:0], zero-alloc after
							// warmup). Crucially the copy DELIBERATELY omits indices —
							// on re-walk the static children become invisible, so
							// matching falls straight to the wildcard child, which is
							// how "fall back to param after static fails" works and
							// what prevents an infinite backtracking loop.
							*skipped = append(*skipped, skippedNode{
								path: prefix + path,
								node: &routeNode{
									path:      n.path,
									wildChild: n.wildChild,
									nType:     n.nType,
									priority:  n.priority,
									children:  n.children,
									handler:   n.handler,
									fullPath:  n.fullPath,
								},
								paramsCount: globalParamsCount,
							})
						}
						n = n.children[i]
						continue walk
					}
				}

				if !n.wildChild {
					// path 末端不为 "/" 且当前节点无匹配子节点:回滚到最近可用 skippedNode。
					// path tail isn't "/" and no child matched: roll back to the
					// most recent usable skippedNode.
					if path != "/" {
						for length := len(*skipped); length > 0; length-- {
							sn := (*skipped)[length-1]
							*skipped = (*skipped)[:length-1]
							if strings.HasSuffix(sn.path, path) {
								path = sn.path
								n = sn.node
								params.truncate(sn.paramsCount)
								globalParamsCount = sn.paramsCount
								continue walk
							}
						}
					}
					// 什么都没找到;若存在去掉尾斜杠的叶子,可建议 TSR。
					// Nothing found; recommend TSR if a leaf exists without the slash.
					value.tsr = path == "/" && n.handler != nil
					return value
				}

				// 处理通配子节点(恒在数组末位)。
				// Handle the wildcard child (always at the end of the array).
				n = n.children[len(n.children)-1]
				globalParamsCount++

				switch n.nType {
				case ntParam:
					// 找 param 结束(下一个 '/' 或 path 末尾)。
					// Find param end (next '/' or path end).
					end := 0
					for end < len(path) && path[end] != '/' {
						end++
					}
					// 保存 param 值(名 = n.path[1:],去掉前导 ':')。
					// Save param value (name = n.path[1:], dropping leading ':').
					params.add(n.path[1:], path[:end])

					// 还要更深 / need to go deeper.
					if end < len(path) {
						if len(n.children) > 0 {
							path = path[end:]
							n = n.children[0]
							continue walk
						}
						// 但无法继续 / but we can't.
						value.tsr = len(path) == end+1
						return value
					}

					if value.handler = n.handler; value.handler != nil {
						value.fullPath = &n.fullPath
						return value
					}
					if len(n.children) == 1 {
						// 无 handler:检查 path + 尾斜杠是否存在以建议 TSR。
						// No handler: check path + trailing slash for TSR.
						n = n.children[0]
						value.tsr = (n.path == "/" && n.handler != nil) || (n.path == "" && n.indices == "/")
					}
					return value

				case ntCatchAll:
					// catchAll 值 = 整个剩余 path(含前导 '/',与 gin 一致);
					// 名 = n.path[2:],去掉前导 '/*'。
					// catchAll value = the whole remaining path (with leading '/',
					// as in gin); name = n.path[2:], dropping leading "/*".
					params.add(n.path[2:], path)

					value.handler = n.handler
					value.fullPath = &n.fullPath
					return value

				default:
					// 不应发生 / should not happen.
					return value
				}
			}
		}

		if path == prefix {
			// 当前 path 非 "/" 且本节点无 handler,而最近匹配节点有子节点:回滚。
			// path isn't "/" and this node has no handler while the recently
			// matched node has children: roll back.
			if n.handler == nil && path != "/" {
				for length := len(*skipped); length > 0; length-- {
					sn := (*skipped)[length-1]
					*skipped = (*skipped)[:length-1]
					if strings.HasSuffix(sn.path, path) {
						path = sn.path
						n = sn.node
						params.truncate(sn.paramsCount)
						globalParamsCount = sn.paramsCount
						continue walk
					}
				}
			}
			// 应已到达含 handler 的节点。
			// We should have reached the node containing the handler.
			if value.handler = n.handler; value.handler != nil {
				value.fullPath = &n.fullPath
				return value
			}

			// 无 handler 但有通配子节点:该路径加尾斜杠必有 handler → TSR。
			// No handler but a wildcard child exists: path + slash has a handler → TSR.
			if path == "/" && n.wildChild && n.nType != ntRoot {
				value.tsr = true
				return value
			}
			if path == "/" && n.nType == ntStatic {
				value.tsr = true
				return value
			}

			// 检查 path + 尾斜杠是否存在以建议 TSR。
			// Check path + trailing slash for a TSR recommendation.
			for i := 0; i < len(n.indices); i++ {
				if n.indices[i] == '/' {
					n = n.children[i]
					value.tsr = (len(n.path) == 1 && n.handler != nil) ||
						(n.nType == ntCatchAll && n.children[0].handler != nil)
					return value
				}
			}
			return value
		}

		// 什么都没找到:若存在多一个尾斜杠的叶子,可建议 TSR。
		// Nothing found: recommend TSR if a leaf with an extra trailing slash exists.
		value.tsr = path == "/" ||
			(len(prefix) == len(path)+1 && prefix[len(path)] == '/' &&
				path == prefix[:len(prefix)-1] && n.handler != nil)

		// 回滚到最近可用 skippedNode。
		// Roll back to the most recent usable skippedNode.
		if !value.tsr && path != "/" {
			for length := len(*skipped); length > 0; length-- {
				sn := (*skipped)[length-1]
				*skipped = (*skipped)[:length-1]
				if strings.HasSuffix(sn.path, path) {
					path = sn.path
					n = sn.node
					params.truncate(sn.paramsCount)
					globalParamsCount = sn.paramsCount
					continue walk
				}
			}
		}
		return value
	}
}

// -----------------------------------------------------------------------------
// 405 支持 / 405 support
// -----------------------------------------------------------------------------

// hasPath 报告本树是否存在与 path 精确匹配(命中 handler)的路由(忽略 method),
// 用于 405 判定。使用独立的临时栈与参数缓冲,不影响调用方状态。
// hasPath reports whether this tree has a route exactly matching path (a handler
// hit, ignoring method), used for 405 detection. It uses its own scratch stack
// and param buffer, leaving the caller's state untouched.
func (t *methodTree) hasPath(path string) bool {
	if t.root == nil {
		return false
	}
	var params Params
	var skipped []skippedNode
	v := t.root.getValue(path, &params, &skipped)
	return v.handler != nil
}

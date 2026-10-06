// Copyright 2013 Julien Schmidt. All rights reserved.
// Adapted from httprouter/gin implementation for ghttp v3
// Use of this source code is governed by a BSD-style license.

package ghttp

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// nodeType 表示节点类型。
// nodeType represents the type of node.
type nodeType uint8

const (
	// static 静态路由节点
	// static route node
	static nodeType = iota
	// root 根节点
	// root node
	root
	// param 参数路由节点（:param）
	// param route node (:param)
	param
	// catchAll 通配路由节点（*param）
	// catchAll route node (*param)
	catchAll
)

// radixNode 是 Gin 风格 radix tree 的节点。
// radixNode is a node in the Gin-style radix tree.
//
// 这是用于 v3 路由器的新实现，与现有的 node 结构分离。
// This is a new implementation for v3 router, separate from the existing node structure.
type radixNode struct {
	// path 节点路径段
	// path is the path segment for this node
	path string

	// indices 子节点首字符索引字符串
	// indices is a string of the first character of each child node's path
	indices string

	// wildChild 是否有通配子节点
	// wildChild indicates whether this node has a wildcard child
	wildChild bool

	// nType 节点类型
	// nType is the type of this node
	nType nodeType

	// priority 优先级（注册次数）
	// priority is the priority (number of registrations)
	priority uint32

	// children 子节点列表（通配节点始终在末尾）
	// children is the list of child nodes (wildcard node always at the end)
	children []*radixNode

	// handler 路由处理器（叶子节点）
	// handler is the route handler (leaf nodes only)
	handler Handler

	// fullPath 完整路径
	// fullPath is the complete path
	fullPath string
}

// longestCommonPrefix 查找两个字符串的最长公共前缀。
// longestCommonPrefix finds the longest common prefix of two strings.
func longestCommonPrefix(a, b string) int {
	i := 0
	max := min(len(a), len(b))
	for i < max && a[i] == b[i] {
		i++
	}
	return i
}

// addChild 添加子节点，保持通配子节点在末尾。
// addChild adds a child node, keeping wildcardChild at the end.
func (n *radixNode) addChild(child *radixNode) {
	if n.wildChild && len(n.children) > 0 {
		wildcardChild := n.children[len(n.children)-1]
		n.children = append(n.children[:len(n.children)-1], child, wildcardChild)
	} else {
		n.children = append(n.children, child)
	}
}

// incrementChildPrio 增加给定子节点的优先级，必要时重新排序。
// incrementChildPrio increments the priority of the given child and reorders it if necessary.
func (n *radixNode) incrementChildPrio(pos int) int {
	cs := n.children
	cs[pos].priority++
	prio := cs[pos].priority

	// 调整位置（移到前面）
	// Adjust position (move to front)
	newPos := pos
	for ; newPos > 0 && cs[newPos-1].priority < prio; newPos-- {
		// 交换节点位置
		// Swap node positions
		cs[newPos-1], cs[newPos] = cs[newPos], cs[newPos-1]
	}

	// 构建新的索引字符串
	// Build new index char string
	if newPos != pos {
		n.indices = n.indices[:newPos] + // 不变的前缀，可能为空
			n.indices[pos:pos+1] + // 我们移动的索引字符
			n.indices[newPos:pos] + n.indices[pos+1:] // 不包含 'pos' 处字符的其余部分
	}

	return newPos
}

// addRoute 添加路径和处理器到节点。
// addRoute adds a node with the given handle to the path.
//
// 非并发安全！
// Not concurrency-safe!
func (n *radixNode) addRoute(path string, handler Handler) {
	fullPath := path
	n.priority++

	// 空树
	// Empty tree
	if len(n.path) == 0 && len(n.children) == 0 {
		n.insertChild(path, fullPath, handler)
		n.nType = root
		return
	}

	parentFullPathIndex := 0

walk:
	for {
		// 查找最长公共前缀
		// Find the longest common prefix.
		i := longestCommonPrefix(path, n.path)

		// 分割边
		// Split edge
		if i < len(n.path) {
			child := radixNode{
				path:      n.path[i:],
				wildChild: n.wildChild,
				nType:     static,
				indices:   n.indices,
				children:  n.children,
				handler:   n.handler,
				priority:  n.priority - 1,
				fullPath:  n.fullPath,
			}

			n.children = []*radixNode{&child}
			n.indices = string([]byte{n.path[i]})
			n.path = path[:i]
			n.handler = nil
			n.wildChild = false
			n.fullPath = fullPath[:parentFullPathIndex+i]
		}

		// 使新节点成为此节点的子节点
		// Make new node a child of this node
		if i < len(path) {
			path = path[i:]
			c := path[0]

			// '/' after param
			if n.nType == param && c == '/' && len(n.children) == 1 {
				parentFullPathIndex += len(n.path)
				n = n.children[0]
				n.priority++
				continue walk
			}

			// 检查是否存在具有下一个路径字节的子节点
			// Check if a child with the next path byte exists
			for i, max := 0, len(n.indices); i < max; i++ {
				if c == n.indices[i] {
					parentFullPathIndex += len(n.path)
					i = n.incrementChildPrio(i)
					n = n.children[i]
					continue walk
				}
			}

			// 否则插入它
			// Otherwise insert it
			if c != ':' && c != '*' && n.nType != catchAll {
				n.indices += string([]byte{c})
				child := &radixNode{
					fullPath: fullPath,
				}
				n.addChild(child)
				n.incrementChildPrio(len(n.indices) - 1)
				n = child
			} else if n.wildChild {
				// 插入通配节点，需要检查是否与现有通配冲突
				// inserting a wildcard node, need to check if it conflicts with the existing wildcard
				n = n.children[len(n.children)-1]
				n.priority++

				// 检查通配是否匹配
				// Check if the wildcard matches
				if len(path) >= len(n.path) && n.path == path[:len(n.path)] &&
					n.nType != catchAll &&
					(len(n.path) >= len(path) || path[len(n.path)] == '/') {
					continue walk
				}

				// 通配冲突
				// Wildcard conflict
				pathSeg := path
				if n.nType != catchAll {
					pathSeg, _, _ = strings.Cut(pathSeg, "/")
				}
				prefix := fullPath[:strings.Index(fullPath, pathSeg)] + n.path
				panic("'" + pathSeg +
					"' in new path '" + fullPath +
					"' conflicts with existing wildcard '" + n.path +
					"' in existing prefix '" + prefix +
					"'")
			}

			n.insertChild(path, fullPath, handler)
			return
		}

		// 否则将处理器添加到当前节点
		// Otherwise add handle to current node
		if n.handler != nil {
			panic("a handler is already registered for path '" + fullPath + "'")
		}
		n.handler = handler
		n.fullPath = fullPath
		return
	}
}

// findWildcard 搜索通配符段并检查名称中的无效字符。
// findWildcard searches for a wildcard segment and checks the name for invalid characters.
func findWildcard(path string) (wildcard string, i int, valid bool) {
	// 查找起始位置
	// Find start
	escapeColon := false
	for start, c := range []byte(path) {
		if escapeColon {
			escapeColon = false
			if c == ':' {
				continue
			}
			panic("invalid escape string in path '" + path + "'")
		}
		if c == '\\' {
			escapeColon = true
			continue
		}
		// 通配符以 ':' (param) 或 '*' (catch-all) 开始
		// A wildcard starts with ':' (param) or '*' (catch-all)
		if c != ':' && c != '*' {
			continue
		}

		// 查找结束位置并检查无效字符
		// Find end and check for invalid characters
		valid = true
		for end, c := range []byte(path[start+1:]) {
			switch c {
			case '/':
				return path[start : start+1+end], start, valid
			case ':', '*':
				valid = false
			}
		}
		return path[start:], start, valid
	}
	return "", -1, false
}

// insertChild 插入子节点到路径。
// insertChild inserts a child node into the path.
func (n *radixNode) insertChild(path string, fullPath string, handler Handler) {
	for {
		// 查找前缀直到第一个通配符
		// Find prefix until first wildcard
		wildcard, i, valid := findWildcard(path)
		if i < 0 { // 没有找到通配符
			break
		}

		// 通配符名称必须只包含一个 ':' 或 '*' 字符
		// The wildcard name must only contain one ':' or '*' character
		if !valid {
			panic("only one wildcard per path segment is allowed, has: '" +
				wildcard + "' in path '" + fullPath + "'")
		}

		// 检查通配符是否有名称
		// check if the wildcard has a name
		if len(wildcard) < 2 {
			panic("wildcards must be named with a non-empty name in path '" + fullPath + "'")
		}

		if wildcard[0] == ':' { // param
			if i > 0 {
				// 在当前通配符之前插入前缀
				// Insert prefix before the current wildcard
				n.path = path[:i]
				path = path[i:]
			}

			child := &radixNode{
				nType:    param,
				path:     wildcard,
				fullPath: fullPath,
			}
			n.addChild(child)
			n.wildChild = true
			n = child
			n.priority++

			// 如果路径没有以通配符结束，那么将有另一个以 '/' 开始的子路径
			// if the path doesn't end with the wildcard, then there
			// will be another subpath starting with '/'
			if len(wildcard) < len(path) {
				path = path[len(wildcard):]

				child := &radixNode{
					priority: 1,
					fullPath: fullPath,
				}
				n.addChild(child)
				n = child
				continue
			}

			// 否则我们完成了，在新叶子中插入处理器
			// Otherwise we're done. Insert the handle in the new leaf
			n.handler = handler
			return
		}

		// catchAll
		if i+len(wildcard) != len(path) {
			panic("catch-all routes are only allowed at the end of the path in path '" + fullPath + "'")
		}

		if len(n.path) > 0 && n.path[len(n.path)-1] == '/' {
			pathSeg := ""
			if len(n.children) != 0 {
				pathSeg, _, _ = strings.Cut(n.children[0].path, "/")
			}
			panic("catch-all wildcard '" + path +
				"' in new path '" + fullPath +
				"' conflicts with existing path segment '" + pathSeg +
				"' in existing prefix '" + n.path + pathSeg +
				"'")
		}

		// 当前固定宽度 1 for '/'
		// currently fixed width 1 for '/'
		i--
		if i < 0 || path[i] != '/' {
			panic("no / before catch-all in path '" + fullPath + "'")
		}

		n.path = path[:i]

		// 第一个节点：带空路径的 catchAll 节点
		// First node: catchAll node with empty path
		child := &radixNode{
			wildChild: true,
			nType:     catchAll,
			fullPath:  fullPath,
		}

		n.addChild(child)
		n.indices = "/"
		n = child
		n.priority++

		// 第二个节点：持有变量的节点
		// second node: node holding the variable
		child = &radixNode{
			path:     path[i:],
			nType:    catchAll,
			handler:  handler,
			priority: 1,
			fullPath: fullPath,
		}
		n.children = []*radixNode{child}

		return
	}

	// 如果没有找到通配符，简单插入路径和处理器
	// If no wildcard was found, simply insert the path and handle
	n.path = path
	n.handler = handler
	n.fullPath = fullPath
}

// nodeValue 保存 (*radixNode).getValue 方法的返回值。
// nodeValue holds return values of (*radixNode).getValue method.
type nodeValue struct {
	handler  Handler
	params   *Params
	tsr      bool
	fullPath string
}

// skippedNode 用于在回溯时保存跳过的节点。
// skippedNode is used to save skipped nodes for backtracking.
type skippedNode struct {
	path        string
	node        *radixNode
	paramsCount int16
}

func lenOrZero(nodes *[]skippedNode) int {
	if nodes == nil {
		return 0
	}
	return len(*nodes)
}

// getValue 返回注册到给定路径的处理器。
// getValue returns the handle registered with the given path (key).
func (n *radixNode) getValue(path string, params *Params, skippedNodes *[]skippedNode, unescape bool) (value nodeValue) {
	var globalParamsCount int16

	// 在入口时重置跳过节点栈。
	// Reset the skipped-nodes stack on entry.
	if skippedNodes != nil {
		*skippedNodes = (*skippedNodes)[:0]
	}

walk: // 遍历树的外层循环
	for {
		prefix := n.path
		if len(path) > len(prefix) {
			if path[:len(prefix)] == prefix {
				path = path[len(prefix):]

				// 首先尝试所有非通配符子节点
				// Try all the non-wildcard children first by matching the indices
				idxc := path[0]
				for i, c := range []byte(n.indices) {
					if c == idxc {
						// 保存跳过的节点以便回溯
						// Save skipped nodes for backtracking
						if n.wildChild && skippedNodes != nil {
							index := len(*skippedNodes)
							*skippedNodes = (*skippedNodes)[:index+1]
							(*skippedNodes)[index] = skippedNode{
								path: prefix + path,
								node: &radixNode{
									path:      n.path,
									wildChild: n.wildChild,
									nType:     n.nType,
									priority:  n.priority,
									children:  n.children,
									handler:   n.handler,
									fullPath:  n.fullPath,
								},
								paramsCount: globalParamsCount,
							}
						}

						n = n.children[i]
						continue walk
					}
				}

				if !n.wildChild {
					if path != "/" {
						for length := lenOrZero(skippedNodes); length > 0; length-- {
							skippedNode := (*skippedNodes)[length-1]
							*skippedNodes = (*skippedNodes)[:length-1]
							if strings.HasSuffix(skippedNode.path, path) {
								path = skippedNode.path
								n = skippedNode.node
								if value.params != nil {
									*value.params = (*value.params)[:skippedNode.paramsCount]
								}
								globalParamsCount = skippedNode.paramsCount
								continue walk
							}
						}
					}

					value.tsr = path == "/" && n.handler != nil
					return value
				}

				// 处理通配符子节点，它总是在数组末尾
				// Handle wildcard child, which is always at the end of the array
				n = n.children[len(n.children)-1]
				globalParamsCount++

				switch n.nType {
				case param:
					// 查找参数结束位置（'/' 或路径结束）
					// Find param end (either '/' or path end)
					end := 0
					for end < len(path) && path[end] != '/' {
						end++
					}

					// 保存参数值
					// Save param value
					if params != nil {
						if cap(*params) < int(globalParamsCount) {
							newParams := make(Params, len(*params), globalParamsCount)
							copy(newParams, *params)
							*params = newParams
						}

						if value.params == nil {
							value.params = params
						}
						i := len(*value.params)
						*value.params = (*value.params)[:i+1]
						val := path[:end]
						if unescape {
							if v, err := url.QueryUnescape(val); err == nil {
								val = v
							}
						}
						(*value.params)[i] = Param{
							Key:   n.path[1:],
							Value: val,
						}
					}

					if end < len(path) {
						if len(n.children) > 0 {
							path = path[end:]
							n = n.children[0]
							continue walk
						}

						value.tsr = len(path) == end+1
						return value
					}

					if value.handler = n.handler; value.handler != nil {
						value.fullPath = n.fullPath
						return value
					}
					if len(n.children) == 1 {
						n = n.children[0]
						value.tsr = (n.path == "/" && n.handler != nil) || (n.path == "" && n.indices == "/")
					}
					return value

				case catchAll:
					if params != nil {
						if cap(*params) < int(globalParamsCount) {
							newParams := make(Params, len(*params), globalParamsCount)
							copy(newParams, *params)
							*params = newParams
						}

						if value.params == nil {
							value.params = params
						}
						i := len(*value.params)
						*value.params = (*value.params)[:i+1]
						val := path
						if unescape {
							if v, err := url.QueryUnescape(path); err == nil {
								val = v
							}
						}
						(*value.params)[i] = Param{
							Key:   n.path[2:],
							Value: val,
						}
					}

					value.handler = n.handler
					value.fullPath = n.fullPath
					return value

				default:
					panic("invalid node type")
				}
			}
		}

		if path == prefix {
			if n.handler == nil && path != "/" {
				for length := lenOrZero(skippedNodes); length > 0; length-- {
					skippedNode := (*skippedNodes)[length-1]
					*skippedNodes = (*skippedNodes)[:length-1]
					if strings.HasSuffix(skippedNode.path, path) {
						path = skippedNode.path
						n = skippedNode.node
						if value.params != nil {
							*value.params = (*value.params)[:skippedNode.paramsCount]
						}
						globalParamsCount = skippedNode.paramsCount
						continue walk
					}
				}
			}

			if value.handler = n.handler; value.handler != nil {
				value.fullPath = n.fullPath
				return value
			}

			if path == "/" && n.wildChild && n.nType != root {
				value.tsr = true
				return value
			}

			if path == "/" && n.nType == static {
				value.tsr = true
				return value
			}

			for i, c := range []byte(n.indices) {
				if c == '/' {
					n = n.children[i]
					value.tsr = (len(n.path) == 1 && n.handler != nil) ||
						(n.nType == catchAll && n.children[0].handler != nil)
					return value
				}
			}

			return value
		}

		value.tsr = path == "/" ||
			(len(prefix) == len(path)+1 && prefix[len(path)] == '/' &&
				path == prefix[:len(prefix)-1] && n.handler != nil)

		if !value.tsr && path != "/" {
			for length := lenOrZero(skippedNodes); length > 0; length-- {
				skippedNode := (*skippedNodes)[length-1]
				*skippedNodes = (*skippedNodes)[:length-1]
				if strings.HasSuffix(skippedNode.path, path) {
					path = skippedNode.path
					n = skippedNode.node
					if value.params != nil {
						*value.params = (*value.params)[:skippedNode.paramsCount]
					}
					globalParamsCount = skippedNode.paramsCount
					continue walk
				}
			}
		}

		return value
	}
}

// findCaseInsensitivePath 对给定路径进行不区分大小写的查找并尝试查找处理器。
// findCaseInsensitivePath makes a case-insensitive lookup of the given path and tries to find a handler.
func (n *radixNode) findCaseInsensitivePath(path string, fixTrailingSlash bool) ([]byte, bool) {
	const stackBufSize = 128

	buf := make([]byte, 0, max(stackBufSize, len(path)+1))

	ciPath := n.findCaseInsensitivePathRec(
		path,
		buf,
		[4]byte{},
		fixTrailingSlash,
	)

	return ciPath, ciPath != nil
}

// shiftNRuneBytes 将数组中的字节向左移动 n 个位置。
// shiftNRuneBytes shifts bytes in the array n positions to the left.
func shiftNRuneBytes(rb [4]byte, n int) [4]byte {
	switch n {
	case 0:
		return rb
	case 1:
		return [4]byte{rb[1], rb[2], rb[3], 0}
	case 2:
		return [4]byte{rb[2], rb[3]}
	case 3:
		return [4]byte{rb[3]}
	default:
		return [4]byte{}
	}
}

// findCaseInsensitivePathRec 递归执行不区分大小写的路径查找。
// findCaseInsensitivePathRec recursively performs a case-insensitive path lookup.
func (n *radixNode) findCaseInsensitivePathRec(path string, ciPath []byte, rb [4]byte, fixTrailingSlash bool) []byte {
	npLen := len(n.path)

walk:
	for len(path) >= npLen && (npLen == 0 || strings.EqualFold(path[1:npLen], n.path[1:])) {
		oldPath := path
		path = path[npLen:]
		ciPath = append(ciPath, n.path...)

		if len(path) == 0 {
			if n.handler != nil {
				return ciPath
			}

			if fixTrailingSlash {
				for i, c := range []byte(n.indices) {
					if c == '/' {
						n = n.children[i]
						if (len(n.path) == 1 && n.handler != nil) ||
							(n.nType == catchAll && n.children[0].handler != nil) {
							return append(ciPath, '/')
						}
						return nil
					}
				}
			}
			return nil
		}

		if !n.wildChild {
			rb = shiftNRuneBytes(rb, npLen)

			if rb[0] != 0 {
				idxc := rb[0]
				for i, c := range []byte(n.indices) {
					if c == idxc {
						n = n.children[i]
						npLen = len(n.path)
						continue walk
					}
				}
			} else {
				var rv rune

				var off int
				for max := min(npLen, 3); off < max; off++ {
					if i := npLen - off; utf8.RuneStart(oldPath[i]) {
						rv, _ = utf8.DecodeRuneInString(oldPath[i:])
						break
					}
				}

				lo := unicode.ToLower(rv)
				utf8.EncodeRune(rb[:], lo)

				rb = shiftNRuneBytes(rb, off)

				idxc := rb[0]
				for i, c := range []byte(n.indices) {
					if c == idxc {
						if out := n.children[i].findCaseInsensitivePathRec(
							path, ciPath, rb, fixTrailingSlash,
						); out != nil {
							return out
						}
						break
					}
				}

				if up := unicode.ToUpper(rv); up != lo {
					utf8.EncodeRune(rb[:], up)
					rb = shiftNRuneBytes(rb, off)

					idxc := rb[0]
					for i, c := range []byte(n.indices) {
						if c == idxc {
							n = n.children[i]
							npLen = len(n.path)
							continue walk
						}
					}
				}
			}

			if fixTrailingSlash && path == "/" && n.handler != nil {
				return ciPath
			}
			return nil
		}

		if len(n.indices) > 0 {
			rb = shiftNRuneBytes(rb, npLen)

			if rb[0] != 0 {
				idxc := rb[0]
				for i, c := range []byte(n.indices) {
					if c == idxc {
						if out := n.children[i].findCaseInsensitivePathRec(
							path, ciPath, rb, fixTrailingSlash,
						); out != nil {
							return out
						}
						break
					}
				}
			} else {
				var rv rune
				var off int
				for max := min(npLen, 3); off < max; off++ {
					if i := npLen - off; utf8.RuneStart(oldPath[i]) {
						rv, _ = utf8.DecodeRuneInString(oldPath[i:])
						break
					}
				}

				lo := unicode.ToLower(rv)
				utf8.EncodeRune(rb[:], lo)
				rb = shiftNRuneBytes(rb, off)

				idxc := rb[0]
				for i, c := range []byte(n.indices) {
					if c == idxc {
						if out := n.children[i].findCaseInsensitivePathRec(
							path, ciPath, rb, fixTrailingSlash,
						); out != nil {
							return out
						}
						break
					}
				}

				if up := unicode.ToUpper(rv); up != lo {
					utf8.EncodeRune(rb[:], up)
					rb = shiftNRuneBytes(rb, off)

					idxc := rb[0]
					for i, c := range []byte(n.indices) {
						if c == idxc {
							if out := n.children[i].findCaseInsensitivePathRec(
								path, ciPath, rb, fixTrailingSlash,
							); out != nil {
								return out
							}
							break
						}
					}
				}
			}
		}

		n = n.children[len(n.children)-1]
		switch n.nType {
		case param:
			end := 0
			for end < len(path) && path[end] != '/' {
				end++
			}

			ciPath = append(ciPath, path[:end]...)

			if end < len(path) {
				if len(n.children) > 0 {
					n = n.children[0]
					npLen = len(n.path)
					path = path[end:]
					continue
				}

				if fixTrailingSlash && len(path) == end+1 {
					return ciPath
				}
				return nil
			}

			if n.handler != nil {
				return ciPath
			}

			if fixTrailingSlash && len(n.children) == 1 {
				n = n.children[0]
				if n.path == "/" && n.handler != nil {
					return append(ciPath, '/')
				}
			}

			return nil

		case catchAll:
			return append(ciPath, path...)

		default:
			panic("invalid node type")
		}
	}

	if fixTrailingSlash {
		if path == "/" {
			return ciPath
		}
		if len(path)+1 == npLen && n.path[len(path)] == '/' &&
			strings.EqualFold(path[1:], n.path[1:len(path)]) && n.handler != nil {
			return append(ciPath, n.path...)
		}
	}
	return nil
}

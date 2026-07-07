package ghttp

import (
	"net/http"
	"strings"
)

const radixStaticIndexThreshold = 8

// routeEntry stores a single route entry.
type routeEntry struct {
	path       string
	handler    http.Handler
	paramNames []string
}

func newRouteEntry(path string, handler http.Handler) *routeEntry {
	return &routeEntry{
		path:       path,
		handler:    handler,
		paramNames: extractParamNames(path),
	}
}

type radixStaticChild struct {
	segment string
	node    *CompressedRadixNode
}

// CompressedRadixNode is a node in the compressed radix tree.
type CompressedRadixNode struct {
	prefix         string
	staticChildren []radixStaticChild
	staticIndex    map[string]*CompressedRadixNode
	paramChild     *CompressedRadixNode
	paramName      string
	wildcardChild  *CompressedRadixNode
	entry          *routeEntry
}

// CompressedRadixTree is a compressed prefix tree for HTTP path matching.
type CompressedRadixTree struct {
	root *CompressedRadixNode
}

func NewCompressedRadixTree() *CompressedRadixTree {
	return newCompressedRadixTree()
}

func newCompressedRadixTree() *CompressedRadixTree {
	return &CompressedRadixTree{
		root: &CompressedRadixNode{},
	}
}

func (t *CompressedRadixTree) insert(entry *routeEntry) {
	segments := splitPathSegments(entry.path)

	if len(segments) == 0 {
		// root path
		t.root.entry = entry
		return
	}
	node := t.root

	for i, seg := range segments {
		if strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "*") {
			isWildcard := strings.HasPrefix(seg, "*")
			paramName := seg[1:]

			if isWildcard {
				if node.wildcardChild == nil {
					node.wildcardChild = &CompressedRadixNode{}
				}
				node.wildcardChild.entry = entry
				node.wildcardChild.prefix = seg
				return
			}

			if node.paramChild == nil {
				node.paramChild = &CompressedRadixNode{}
			}
			node.paramChild.paramName = paramName
			if i == len(segments)-1 {
				node.paramChild.entry = entry
			}
			node = node.paramChild
			continue
		}

		// Static segment
		child := node.staticChild(seg)
		if child != nil {
			node = child
			if i == len(segments)-1 {
				node.entry = entry
			}
			continue
		}

		child = &CompressedRadixNode{
			prefix: seg,
		}
		if i == len(segments)-1 {
			child.entry = entry
		}
		node.addStaticChild(seg, child)
		node = child
	}
}

func (n *CompressedRadixNode) staticChild(segment string) *CompressedRadixNode {
	if n.staticIndex != nil {
		return n.staticIndex[segment]
	}
	for i := range n.staticChildren {
		child := &n.staticChildren[i]
		if child.segment == segment {
			return child.node
		}
	}
	return nil
}

func (n *CompressedRadixNode) addStaticChild(segment string, child *CompressedRadixNode) {
	n.staticChildren = append(n.staticChildren, radixStaticChild{
		segment: segment,
		node:    child,
	})
	if n.staticIndex != nil {
		n.staticIndex[segment] = child
		return
	}
	if len(n.staticChildren) < radixStaticIndexThreshold {
		return
	}
	n.staticIndex = make(map[string]*CompressedRadixNode, len(n.staticChildren))
	for i := range n.staticChildren {
		staticChild := &n.staticChildren[i]
		n.staticIndex[staticChild.segment] = staticChild.node
	}
}

func (n *CompressedRadixNode) removeStaticChild(segment string) {
	for i := range n.staticChildren {
		if n.staticChildren[i].segment != segment {
			continue
		}
		copy(n.staticChildren[i:], n.staticChildren[i+1:])
		n.staticChildren[len(n.staticChildren)-1] = radixStaticChild{}
		n.staticChildren = n.staticChildren[:len(n.staticChildren)-1]
		break
	}
	if n.staticIndex == nil {
		return
	}
	delete(n.staticIndex, segment)
	if len(n.staticChildren) < radixStaticIndexThreshold {
		n.staticIndex = nil
	}
}

func (n *CompressedRadixNode) empty() bool {
	return n.entry == nil &&
		len(n.staticChildren) == 0 &&
		n.paramChild == nil &&
		n.wildcardChild == nil
}

func (t *CompressedRadixTree) lookup(path string, params *pathParamList) *routeEntry {
	return t.lookupRecursive(t.root, path, 0, params)
}

func (t *CompressedRadixTree) lookupRecursive(node *CompressedRadixNode, path string, index int, params *pathParamList) *routeEntry {
	seg, next, ok := nextPathSegment(path, index)
	if !ok {
		if node.entry != nil {
			return node.entry
		}
		return nil
	}

	if child := node.staticChild(seg); child != nil {
		if result := t.lookupRecursive(child, path, next, params); result != nil {
			return result
		}
	}

	if node.paramChild != nil {
		paramCount := params.Len()
		params.Add(node.paramChild.paramName, seg)
		if result := t.lookupRecursive(node.paramChild, path, next, params); result != nil {
			return result
		}
		params.Truncate(paramCount)
	}

	if node.wildcardChild != nil && node.wildcardChild.entry != nil {
		remaining := remainingPath(path, index)
		params.Add(node.wildcardChild.entry.paramNames[0], remaining)
		return node.wildcardChild.entry
	}

	return nil
}

func nextPathSegment(path string, index int) (string, int, bool) {
	for index < len(path) && path[index] == '/' {
		index++
	}
	if index >= len(path) {
		return "", index, false
	}
	end := index
	for end < len(path) && path[end] != '/' {
		end++
	}
	return path[index:end], end, true
}

func remainingPath(path string, index int) string {
	for index < len(path) && path[index] == '/' {
		index++
	}
	return path[index:]
}

func (t *CompressedRadixTree) remove(path string) {
	segments := splitPathSegments(path)
	t.removeRecursive(t.root, segments, 0)
}

func (t *CompressedRadixTree) removeRecursive(node *CompressedRadixNode, segments []string, depth int) bool {
	if depth == len(segments) {
		if node.entry != nil {
			node.entry = nil
			return true
		}
		return false
	}

	seg := segments[depth]
	if strings.HasPrefix(seg, ":") {
		if node.paramChild != nil {
			if t.removeRecursive(node.paramChild, segments, depth+1) {
				if node.paramChild.empty() {
					node.paramChild = nil
				}
				return true
			}
		}
	} else if strings.HasPrefix(seg, "*") {
		if node.wildcardChild != nil {
			node.wildcardChild.entry = nil
			if node.wildcardChild.empty() {
				node.wildcardChild = nil
			}
			return true
		}
	} else {
		child := node.staticChild(seg)
		if child != nil && t.removeRecursive(child, segments, depth+1) {
			if child.empty() {
				node.removeStaticChild(seg)
			}
			return true
		}
	}

	return false
}

func (t *CompressedRadixTree) routeCount() int {
	return t.countRecursive(t.root)
}

func (t *CompressedRadixTree) countRecursive(node *CompressedRadixNode) int {
	count := 0
	if node.entry != nil {
		count++
	}
	for _, child := range node.staticChildren {
		count += t.countRecursive(child.node)
	}
	if node.paramChild != nil {
		count += t.countRecursive(node.paramChild)
	}
	if node.wildcardChild != nil {
		count += t.countRecursive(node.wildcardChild)
	}
	return count
}

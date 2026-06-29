package ghttp

import (
	"net/http"
	"strings"
)

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

// CompressedRadixNode is a node in the compressed radix tree.
type CompressedRadixNode struct {
	prefix        string
	children      map[string]*CompressedRadixNode
	paramChild    *CompressedRadixNode
	paramName     string
	wildcardChild *CompressedRadixNode
	entry         *routeEntry
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
		root: &CompressedRadixNode{
			children: make(map[string]*CompressedRadixNode),
		},
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
					node.wildcardChild = &CompressedRadixNode{
						children: make(map[string]*CompressedRadixNode),
					}
				}
				node.wildcardChild.entry = entry
				node.wildcardChild.prefix = seg
				return
			}

			if node.paramChild == nil {
				node.paramChild = &CompressedRadixNode{
					children: make(map[string]*CompressedRadixNode),
				}
			}
			node.paramChild.paramName = paramName
			if i == len(segments)-1 {
				node.paramChild.entry = entry
			}
			node = node.paramChild
			continue
		}

		// Static segment
		child, exists := node.children[seg]
		if exists {
			node = child
			if i == len(segments)-1 {
				node.entry = entry
			}
			continue
		}

		child = &CompressedRadixNode{
			prefix:   seg,
			children: make(map[string]*CompressedRadixNode),
		}
		if i == len(segments)-1 {
			child.entry = entry
		}
		node.children[seg] = child
		node = child
	}
}

func (t *CompressedRadixTree) lookup(segments []string, params map[string]string) *routeEntry {
	return t.lookupRecursive(t.root, segments, 0, params)
}

func (t *CompressedRadixTree) lookupRecursive(node *CompressedRadixNode, segments []string, depth int, params map[string]string) *routeEntry {
	if depth >= len(segments) {
		if node.entry != nil {
			return node.entry
		}
		return nil
	}

	seg := segments[depth]

	// 1. Try static child
	if node.children != nil {
		if child, ok := node.children[seg]; ok {
			if result := t.lookupRecursive(child, segments, depth+1, params); result != nil {
				return result
			}
		}
	}

	// 2. Try param child
	if node.paramChild != nil {
		if node.paramChild.entry != nil && depth == len(segments)-1 {
			params[node.paramChild.paramName] = seg
			return node.paramChild.entry
		}
		if result := t.lookupRecursive(node.paramChild, segments, depth+1, params); result != nil {
			params[node.paramChild.paramName] = seg
			return result
		}
	}

	// 3. Try wildcard child
	if node.wildcardChild != nil && node.wildcardChild.entry != nil {
		remaining := strings.Join(segments[depth:], "/")
		params[node.wildcardChild.entry.paramNames[0]] = remaining
		return node.wildcardChild.entry
	}

	return nil
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
				if node.paramChild.entry == nil && len(node.paramChild.children) == 0 {
					node.paramChild = nil
				}
				return true
			}
		}
	} else if strings.HasPrefix(seg, "*") {
		if node.wildcardChild != nil {
			node.wildcardChild.entry = nil
			return true
		}
	} else if node.children != nil {
		if child, ok := node.children[seg]; ok {
			if t.removeRecursive(child, segments, depth+1) {
				if child.entry == nil && len(child.children) == 0 {
					delete(node.children, seg)
				}
				return true
			}
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
	for _, child := range node.children {
		count += t.countRecursive(child)
	}
	if node.paramChild != nil {
		count += t.countRecursive(node.paramChild)
	}
	if node.wildcardChild != nil {
		count += t.countRecursive(node.wildcardChild)
	}
	return count
}

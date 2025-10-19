package gserver

import (
	"fmt"
	"strings"
	"sync"
)

type CompressedRadixNode struct {
	children      map[string]*CompressedRadixNode
	paramChild    *CompressedRadixNode
	wildcardChild *CompressedRadixNode
	paramName     string
	entry         *routeEntry
}

type CompressedRadixTree struct {
	root *CompressedRadixNode
	size int
	mu   sync.RWMutex
}

func newCompressedRadixTree() *CompressedRadixTree {
	return &CompressedRadixTree{
		root: newRadixNode(),
	}
}

func newRadixNode() *CompressedRadixNode {
	return &CompressedRadixNode{
		children: make(map[string]*CompressedRadixNode),
	}
}

func (crt *CompressedRadixTree) insert(entry *routeEntry) error {
	if entry == nil {
		return fmt.Errorf("nil route entry")
	}

	crt.mu.Lock()
	defer crt.mu.Unlock()

	if crt.root == nil {
		crt.root = newRadixNode()
	}

	segments := splitPathSegments(entry.path)
	current := crt.root

	if len(segments) == 0 {
		if current.entry != nil {
			return fmt.Errorf("duplicate route: %s", entry.path)
		}
		current.entry = entry
		crt.size++
		return nil
	}

	for i, segment := range segments {
		isLast := i == len(segments)-1
		if len(segment) == 0 {
			continue
		}

		switch segment[0] {
		case ':':
			if current.paramChild == nil {
				current.paramChild = newRadixNode()
				current.paramChild.paramName = segment[1:]
			}
			current = current.paramChild
		case '*':
			if !isLast {
				return fmt.Errorf("catch-all must be the last segment in route %s", entry.path)
			}
			if current.wildcardChild == nil {
				current.wildcardChild = newRadixNode()
				current.wildcardChild.paramName = segment[1:]
			}
			if current.wildcardChild.entry != nil {
				return fmt.Errorf("duplicate route: %s", entry.path)
			}
			current.wildcardChild.entry = entry
			crt.size++
			return nil
		default:
			child, ok := current.children[segment]
			if !ok {
				child = newRadixNode()
				current.children[segment] = child
			}
			current = child
		}
	}

	if current.entry != nil {
		return fmt.Errorf("duplicate route: %s", entry.path)
	}

	current.entry = entry
	crt.size++
	return nil
}

func (crt *CompressedRadixTree) remove(path string) error {
	crt.mu.Lock()
	defer crt.mu.Unlock()

	if crt.root == nil {
		return fmt.Errorf("empty tree")
	}

	removed, _, err := removeRadixNode(crt.root, splitPathSegments(path))
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("not found")
	}

	crt.size--
	return nil
}

func removeRadixNode(current *CompressedRadixNode, segments []string) (bool, bool, error) {
	if len(segments) == 0 {
		if current.entry == nil {
			return false, false, nil
		}
		current.entry = nil
		return true, current.isEmpty(), nil
	}

	segment := segments[0]
	rest := segments[1:]

	if len(segment) > 0 && segment[0] != ':' && segment[0] != '*' {
		if child, ok := current.children[segment]; ok {
			removed, prune, err := removeRadixNode(child, rest)
			if err != nil {
				return false, false, err
			}
			if removed {
				if prune {
					delete(current.children, segment)
				}
				return true, current.isEmpty(), nil
			}
		}
	}

	if len(segment) > 0 && segment[0] == ':' && current.paramChild != nil {
		removed, prune, err := removeRadixNode(current.paramChild, rest)
		if err != nil {
			return false, false, err
		}
		if removed {
			if prune {
				current.paramChild = nil
			}
			return true, current.isEmpty(), nil
		}
	}

	if len(segment) > 0 && segment[0] == '*' && current.wildcardChild != nil {
		if len(rest) == 0 && current.wildcardChild.paramName == segment[1:] && current.wildcardChild.entry != nil {
			current.wildcardChild.entry = nil
			if current.wildcardChild.isEmpty() {
				current.wildcardChild = nil
			}
			return true, current.isEmpty(), nil
		}
	}

	return false, false, nil
}

func (crt *CompressedRadixTree) search(path string) *MatchResult {
	crt.mu.RLock()
	defer crt.mu.RUnlock()

	if crt.root == nil {
		return nil
	}

	segments := splitPathSegments(path)
	var params map[string]string

	entry := crt.root.find(segments, &params)
	if entry == nil {
		return nil
	}

	return entry.toResult(params)
}

func (n *CompressedRadixNode) find(segments []string, params *map[string]string) *routeEntry {
	if len(segments) == 0 {
		if n.entry != nil {
			return n.entry
		}
		if n.wildcardChild != nil && n.wildcardChild.entry != nil {
			values := ensureParams(params)
			values[n.wildcardChild.paramName] = ""
			return n.wildcardChild.entry
		}
		return nil
	}

	segment := segments[0]
	rest := segments[1:]

	if len(segment) > 0 {
		if child, ok := n.children[segment]; ok {
			if entry := child.find(rest, params); entry != nil {
				return entry
			}
		}
	}

	if n.paramChild != nil && len(segment) > 0 {
		values := ensureParams(params)
		values[n.paramChild.paramName] = segment
		if entry := n.paramChild.find(rest, params); entry != nil {
			return entry
		}
		delete(values, n.paramChild.paramName)
	}

	if n.wildcardChild != nil && n.wildcardChild.entry != nil {
		values := ensureParams(params)
		values[n.wildcardChild.paramName] = strings.Join(segments, "/")
		return n.wildcardChild.entry
	}

	return nil
}

func (n *CompressedRadixNode) isEmpty() bool {
	return n.entry == nil &&
		len(n.children) == 0 &&
		n.paramChild == nil &&
		n.wildcardChild == nil
}

func ensureParams(params *map[string]string) map[string]string {
	if *params == nil {
		*params = make(map[string]string)
	}
	return *params
}

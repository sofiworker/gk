package gserver

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type CompressedRadixNode struct {
	prefix     string
	children   []*CompressedRadixNode
	isParam    bool
	isWildcard bool
	paramName  string
	handlers   []HandlerFunc
	priority   uint8
	flags      uint8
}

type CompressedRadixTree struct {
	root *CompressedRadixNode
	size int
	mu   sync.RWMutex
}

func newCompressedRadixTree() *CompressedRadixTree {
	return &CompressedRadixTree{}
}

func (crt *CompressedRadixTree) insert(path string, handler ...HandlerFunc) error {
	crt.mu.Lock()
	defer crt.mu.Unlock()
	if crt.root == nil {
		crt.root = &CompressedRadixNode{}
	}
	currentNode := crt.root
	remaining := path

	for len(remaining) > 0 {
		var found bool
		for _, child := range currentNode.children {
			common := longestCommonPrefix(remaining, child.prefix)
			if common > 0 {
				if common < len(child.prefix) {
					splitNode(child, common)
				}
				currentNode = child
				remaining = strings.TrimPrefix(remaining, child.prefix)
				if strings.HasPrefix(remaining, "/") {
					remaining = remaining[1:]
				}
				found = true
				break
			}
		}
		if !found {
			newNode := crt.createNode(remaining)
			currentNode.children = append(currentNode.children, newNode)
			sortNodes(currentNode.children)
			currentNode = newNode
			break
		}
	}

	// 如果该节点已有 handler，说明重复添加
	if len(currentNode.handlers) > 0 {
		return fmt.Errorf("duplicate route: %s", path)
	}

	currentNode.handlers = handler
	crt.size++
	return nil
}

func (crt *CompressedRadixTree) remove(path string) error {
	crt.mu.Lock()
	defer crt.mu.Unlock()
	if crt.root == nil {
		return fmt.Errorf("empty tree")
	}

	var parents []*CompressedRadixNode
	current := crt.root
	remaining := path

	for len(remaining) > 0 && current != nil {
		var next *CompressedRadixNode
		for _, child := range current.children {
			if strings.HasPrefix(remaining, child.prefix) {
				remaining = strings.TrimPrefix(remaining, child.prefix)
				if strings.HasPrefix(remaining, "/") {
					remaining = remaining[1:]
				}
				parents = append(parents, current)
				next = child
				current = child
				break
			}
		}
		if next == nil {
			return fmt.Errorf("path not found: %s", path)
		}
	}

	if current == nil || len(current.handlers) == 0 {
		return fmt.Errorf("not found")
	}

	current.handlers = nil
	crt.size--

	// 清理无用节点
	for i := len(parents) - 1; i >= 0; i-- {
		p := parents[i]
		for idx, c := range p.children {
			if c == current && len(c.children) == 0 && len(c.handlers) == 0 {
				p.children = append(p.children[:idx], p.children[idx+1:]...)
				current = p
				break
			}
		}
	}

	return nil
}

func (crt *CompressedRadixTree) search(path string) *MatchResult {
	crt.mu.RLock()
	defer crt.mu.RUnlock()

	if crt.root == nil {
		return nil
	}

	current := crt.root
	remaining := path
	params := make(map[string]string)

	for len(remaining) > 0 && current != nil {
		var next *CompressedRadixNode
		var consumed string

		for _, child := range current.children {
			// 通配符：直接吸收剩余路径
			if child.isWildcard {
				params[child.paramName] = remaining
				next = child
				remaining = ""
				break
			}

			// 参数匹配：匹配一段直到 '/'
			if child.isParam {
				end := strings.IndexByte(remaining, '/')
				if end == -1 {
					end = len(remaining)
				}
				params[child.paramName] = remaining[:end]
				next = child
				consumed = remaining[:end]
				remaining = remaining[end:]
				if strings.HasPrefix(remaining, "/") {
					remaining = remaining[1:]
				}
				break
			}

			// 静态匹配
			if strings.HasPrefix(remaining, child.prefix) {
				next = child
				consumed = child.prefix
				remaining = strings.TrimPrefix(remaining, child.prefix)
				if strings.HasPrefix(remaining, "/") {
					remaining = remaining[1:]
				}
				break
			}
		}

		if next == nil {
			return nil
		}

		current = next
		if consumed == "" && !next.isWildcard && !next.isParam {
			break
		}
	}

	if current != nil && len(current.handlers) > 0 {
		return &MatchResult{
			Path:     path,
			Handlers: current.handlers,
			Params:   params,
		}
	}

	return nil
}

func (crt *CompressedRadixTree) createNode(path string) *CompressedRadixNode {
	node := &CompressedRadixNode{}
	if len(path) > 0 {
		switch path[0] {
		case ':':
			node.isParam = true
			end := strings.IndexByte(path, '/')
			if end == -1 {
				end = len(path)
			}
			node.paramName = path[1:end]
			node.prefix = path[:end]
		case '*':
			node.isWildcard = true
			node.paramName = path[1:]
			node.prefix = path
		default:
			end := strings.IndexByte(path, '/')
			if end == -1 {
				end = len(path)
			}
			paramStart := strings.IndexByte(path, ':')
			wildStart := strings.IndexByte(path, '*')
			if paramStart != -1 && (paramStart < end) {
				end = paramStart
			} else if wildStart != -1 && (wildStart < end) {
				end = wildStart
			}
			node.prefix = path[:end]
		}
	}
	return node
}

// 一些工具函数
func splitNode(node *CompressedRadixNode, splitPos int) {
	newChild := &CompressedRadixNode{
		prefix:     node.prefix[splitPos:],
		children:   node.children,
		isParam:    node.isParam,
		isWildcard: node.isWildcard,
		paramName:  node.paramName,
		handlers:   node.handlers,
	}
	node.prefix = node.prefix[:splitPos]
	node.children = []*CompressedRadixNode{newChild}
	node.handlers = nil
}

func sortNodes(nodes []*CompressedRadixNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if !nodes[i].isParam && !nodes[i].isWildcard {
			if nodes[j].isParam || nodes[j].isWildcard {
				return true
			}
		} else if !nodes[j].isParam && !nodes[j].isWildcard {
			return false
		}
		return len(nodes[i].prefix) > len(nodes[j].prefix)
	})
}

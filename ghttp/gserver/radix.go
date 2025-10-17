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

	for remaining != "" {
		var found bool
		for _, child := range currentNode.children {
			common := longestCommonPrefix(remaining, child.prefix)
			if common > 0 {
				if common == len(child.prefix) {
					currentNode = child
					if common <= len(remaining) {
						remaining = remaining[common:]
					} else {
						remaining = ""
					}
					found = true
					break
				} else {
					splitNode(child, common)
					currentNode = child
					if common <= len(remaining) {
						remaining = remaining[common:]
					} else {
						remaining = ""
					}
					found = true
					break
				}
			}
		}
		if !found {
			newNode := crt.createNode(remaining)
			currentNode.children = append(currentNode.children, newNode)
			sortNodes(currentNode.children)
			currentNode = newNode
			remaining = ""
			break
		}
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
	// 简单查找并删除 handler
	current := crt.root
	remaining := path
	var parent *CompressedRadixNode
	var parentIndex int
	for remaining != "" {
		var next *CompressedRadixNode
		var idx int
		for i, child := range current.children {
			if strings.HasPrefix(remaining, child.prefix) || strings.HasPrefix(child.prefix, remaining) {
				next = child
				idx = i
				break
			}
		}
		if next == nil {
			return fmt.Errorf("not found")
		}
		parent = current
		parentIndex = idx
		current = next
		if len(remaining) >= len(current.prefix) {
			remaining = remaining[len(current.prefix):]
		} else {
			remaining = ""
		}
		if len(remaining) > 0 && remaining[0] == '/' {
			remaining = remaining[1:]
		}
	}
	//if current.handler == nil {
	//	return fmt.Errorf("not found")
	//}
	//current.handler = nil
	crt.size--
	// 如果节点没有 handler 且没有子节点，从父节点移除
	if parent != nil && len(current.children) == 0 && current.handlers == nil {
		parent.children = append(parent.children[:parentIndex], parent.children[parentIndex+1:]...)
	}
	return nil
}

func (crt *CompressedRadixTree) search(path string) *MatchResult {
	crt.mu.RLock()
	defer crt.mu.RUnlock()
	if crt.root == nil {
		return nil
	}
	currentNode := crt.root
	remaining := path

	for currentNode != nil && remaining != "" {
		var nextNode *CompressedRadixNode
		var matchLen int

		for _, child := range currentNode.children {
			if child.isParam || child.isWildcard {
				continue
			}
			if len(remaining) >= len(child.prefix) && remaining[:len(child.prefix)] == child.prefix {
				nextNode = child
				matchLen = len(child.prefix)
				break
			}
		}
		if nextNode == nil {
			for _, child := range currentNode.children {
				if child.isParam {
					end := strings.IndexByte(remaining, '/')
					if end == -1 {
						end = len(remaining)
					}
					nextNode = child
					matchLen = end
					break
				} else if child.isWildcard {
					nextNode = child
					matchLen = len(remaining)
					break
				}
			}
		}

		if nextNode == nil {
			return nil
		}

		currentNode = nextNode
		if matchLen <= len(remaining) {
			remaining = remaining[matchLen:]
		} else {
			remaining = ""
		}
		if len(remaining) > 0 && remaining[0] == '/' {
			remaining = remaining[1:]
		}
	}

	//if currentNode != nil && currentNode.handler != nil {
	//	return &MatchResult{}
	//}
	return nil
}

func (crt *CompressedRadixTree) estimateMemory() uint64 {
	// 简单估算
	return uint64(crt.size) * 64
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

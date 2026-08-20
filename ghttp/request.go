package ghttp

import "net/http"

// Params 保存单次请求匹配到的路径参数,使用并行槽位数组而非 map,以避免
// map 分配与哈希开销。
// Params holds the path parameters matched for a single request. It uses
// parallel slot slices instead of a map to avoid map allocation and hashing.
type Params struct {
	keys []string
	vals []string
}

// Get 返回名为 name 的路径参数值;不存在时返回空字符串。
// Get returns the path parameter value named name, or "" if absent.
func (p Params) Get(name string) string {
	for i, k := range p.keys {
		if k == name {
			return p.vals[i]
		}
	}
	return ""
}

// Len 返回已匹配的路径参数个数。
// Len returns the number of matched path parameters.
func (p Params) Len() int { return len(p.keys) }

// reset 清空槽位以便池化复用,保留底层容量。
// reset clears the slots for pooled reuse while keeping the backing capacity.
func (p *Params) reset() {
	p.keys = p.keys[:0]
	p.vals = p.vals[:0]
}

// add 追加一个路径参数键值。
// add appends one path parameter key/value pair.
func (p *Params) add(key, val string) {
	p.keys = append(p.keys, key)
	p.vals = append(p.vals, val)
}

// truncate 回退到 length 个参数,用于匹配回溯时撤销失败分支写入的参数。
// truncate rolls back to length parameters, undoing params written by a failed
// branch during match backtracking.
func (p *Params) truncate(length int) {
	p.keys = p.keys[:length]
	p.vals = p.vals[:length]
}

// Request 是对标准 *http.Request 的轻量封装,额外携带已匹配的路径参数。
// Request is a thin wrapper over the standard *http.Request that additionally
// carries the matched path parameters.
//
// resp 是与本 Request 同生命周期、随池复用的响应封装,避免每请求堆分配 Response。
// skipped 是随池复用的回溯栈(gin getValue 用),避免每请求分配。
// resp is the response wrapper sharing this Request's lifecycle and pool reuse,
// avoiding a per-request Response heap allocation. skipped is the pooled
// backtracking stack (used by gin's getValue), avoiding a per-request alloc.
type Request struct {
	*http.Request
	Params  Params
	resp    Response
	skipped []skippedNode
}

// Response 是对标准 http.ResponseWriter 的轻量封装。
// Response is a thin wrapper over the standard http.ResponseWriter.
type Response struct {
	http.ResponseWriter
}

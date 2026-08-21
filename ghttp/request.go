package ghttp

import (
	"io"
	"net/http"
)

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

// Response 是对标准 http.ResponseWriter 的轻量封装,额外追踪状态码与是否已提交。
// 中间件(如访问日志读状态码)与错误链(判断是否已写以决定能否补写 500)都依赖它。
// Response is a thin wrapper over the standard http.ResponseWriter that also
// tracks the status code and whether the response was committed. Both middleware
// (e.g. access logging reading the status) and the error chain (deciding whether
// a 500 can still be written) depend on it.
type Response struct {
	http.ResponseWriter
	status  int
	written bool
}

// WriteHeader 记录状态码并标记已提交,然后委托底层 writer。
// WriteHeader records the status code, marks the response committed, then
// delegates to the underlying writer.
func (r *Response) WriteHeader(code int) {
	if r.written {
		return
	}
	r.status = code
	r.written = true
	r.ResponseWriter.WriteHeader(code)
}

// Write 在首次写入前隐式提交 200(与标准库语义一致),然后委托底层 writer。
// Write implicitly commits 200 before the first body write (matching stdlib
// semantics), then delegates to the underlying writer.
func (r *Response) Write(b []byte) (int, error) {
	if !r.written {
		r.status = http.StatusOK
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

// WriteString 在首次写入前隐式提交 200,然后委托底层 writer(若其支持 io.StringWriter,
// 否则回退到 []byte 转换),避免字符串写入的额外分配。
// WriteString implicitly commits 200 before the first write, then delegates to
// the underlying writer's io.StringWriter if available (else falls back to a
// []byte conversion), avoiding an allocation for string writes.
func (r *Response) WriteString(s string) (int, error) {
	if !r.written {
		r.status = http.StatusOK
		r.written = true
	}
	if sw, ok := r.ResponseWriter.(io.StringWriter); ok {
		return sw.WriteString(s)
	}
	return r.ResponseWriter.Write([]byte(s))
}

// Status 返回已写入的状态码;未写时返回 0。
// Status returns the written status code, or 0 if nothing was written.
func (r *Response) Status() int { return r.status }

// Written 报告响应是否已提交(状态码或响应体已写)。
// Written reports whether the response has been committed (status or body).
func (r *Response) Written() bool { return r.written }

// reset 清空追踪状态以便池化复用。
// reset clears the tracking state for pooled reuse.
func (r *Response) reset() {
	r.ResponseWriter = nil
	r.status = 0
	r.written = false
}

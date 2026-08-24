package ghttp

import (
	"io"
	"net/http"
	"net/url"
)

// Params 保存单次请求匹配到的路径参数，使用并行槽位数组而非 map，以避免
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

// reset 清空槽位以便池化复用，保留底层容量。
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

// truncate 回退到 length 个参数，用于匹配回溯时撤销失败分支写入的参数。
// truncate rolls back to length parameters, undoing params written by a failed
// branch during match backtracking.
func (p *Params) truncate(length int) {
	p.keys = p.keys[:length]
	p.vals = p.vals[:length]
}

// Request 是对标准 *http.Request 的轻量封装，额外携带已匹配的路径参数。
// 为减少 URL.Query()每请求分配，查询字符串缓存在 queryCache(首次调用 Query()时解析)。
// Request is a thin wrapper over the standard *http.Request that additionally
// carries the matched path parameters. To reduce per-request URL.Query() allocation,
// query string is cached in queryCache (parsed on first Query() call).
type Request struct {
	*http.Request
	Params     Params
	queryCache url.Values // filled on first Query() call; cleared at reset.
	resp       Response
	skipped    []skippedNode
	// matchedRoute 命中的完整路由模板(gin 形式,如 /users/:id),命中后由分发器写入。
	// 未命中或 raw 快路径的 miss 时为空。经 MatchedRoute() 只读暴露。
	// matchedRoute is the matched full route template (gin form, e.g.
	// /users/:id), written by the dispatcher after a hit. Empty on a miss or a
	// raw fast-path miss. Exposed read-only via MatchedRoute().
	matchedRoute string
	// owner 指向借出本 Request 的 mux,在池的 New 闭包中一次性设置、reset 不清空,
	// 使 ClientIP() 等访问器无需每请求写入即可读取 server 级配置。可能为 nil
	// (miss 冷路径构造的临时 Request),访问器需容忍。
	// owner points to the mux that lends this Request, set once in the pool's New
	// closure and never cleared on reset, so accessors like ClientIP() can read
	// server-level config with no per-request write. May be nil (transient
	// Requests built on the miss cold path); accessors must tolerate it.
	owner *mux
}

// MatchedRoute 返回命中的完整路由模板(gin 形式,如 "/users/:id");未命中时返回空串。
// 它是低基数标签,适合作为 metrics/tracing/日志的路由维度,避免用高基数的原始 path。
// MatchedRoute returns the matched full route template (gin form, e.g.
// "/users/:id"); empty on a miss. It is a low-cardinality label suited for the
// route dimension in metrics/tracing/logging, avoiding the high-cardinality path.
func (r *Request) MatchedRoute() string { return r.matchedRoute }

// Query 返回解析后的 URL 查询参数;首次调用时解析并缓存，后续复用该 url.Values 指针避免重新解析。
// Query returns the parsed URL query values; parses and caches on first call,
// reusing the same pointer thereafter to avoid repeated parsing.
func (r *Request) Query() url.Values {
	if r.queryCache == nil {
		r.queryCache = r.URL.Query() // 标准库也会 alloc,但之后复用不重复解析。allocs once, then reused.
	}
	return r.queryCache
}

// reset 清空追踪状态以便池化复用。
// reset clears tracking state for pooled reuse.
func (r *Request) reset() {
	r.queryCache = nil // 清空 cache 供下一请求重新解析。
	r.Params.reset()
	r.skipped = r.skipped[:0]
	r.matchedRoute = ""
	r.Request = nil
}

// Response 是对标准 http.ResponseWriter 的轻量封装，额外追踪状态码与是否已提交。
// 中间件 (如访问日志读状态码)与错误链 (判断是否已写以决定能否补写 500)都依赖它。
// Response is a thin wrapper over the standard http.ResponseWriter that also
// tracks the status code and whether the response was committed. Both middleware
// (e.g. access logging reading the status) and the error chain (deciding whether
// a 500 can still be written) depend on it.
type Response struct {
	http.ResponseWriter
	status   int
	written  bool
	bytesOut int
}

// WriteHeader 记录状态码并标记已提交，然后委托底层 writer。
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
	n, err := r.ResponseWriter.Write(b)
	r.bytesOut += n
	return n, err
}

// WriteString 在首次写入前隐式提交 200，然后委托底层 writer(若其支持 io.StringWriter,
// 否则回退到 []byte 转换),避免字符串写入的额外分配。
// WriteString implicitly commits 200 before the first write, then delegates to
// the underlying writer's io.StringWriter if available (else falls back to a
// []byte conversion), avoiding an allocation for string writes.
func (r *Response) WriteString(s string) (int, error) {
	if !r.written {
		r.status = http.StatusOK
		r.written = true
	}
	var n int
	var err error
	if sw, ok := r.ResponseWriter.(io.StringWriter); ok {
		n, err = sw.WriteString(s)
	} else {
		n, err = r.ResponseWriter.Write([]byte(s))
	}
	r.bytesOut += n
	return n, err
}

// Status 返回已写入的状态码;未写时返回 0。
// Status returns the written status code, or 0 if nothing was written.
func (r *Response) Status() int { return r.status }

// BytesOut 返回经本 Response 写出的响应体字节数(累计),供访问日志记录响应大小。
// BytesOut returns the cumulative response-body bytes written through this
// Response, for access logging of the response size.
func (r *Response) BytesOut() int { return r.bytesOut }

// Written 报告响应是否已提交 (状态码或响应体已写)。
// Written reports whether the response has been committed (status or body).
func (r *Response) Written() bool { return r.written }

// reset 清空追踪状态以便池化复用。
// reset clears the tracking state for pooled reuse.
func (r *Response) reset() {
	r.ResponseWriter = nil
	r.status = 0
	r.written = false
	r.bytesOut = 0
}

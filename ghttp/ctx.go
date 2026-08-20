// Ctx 是 gin/echo 式执行上下文:池化的具体类型,请求→handler 直传,
// 中间件与终端之间零接口调度。Ctx 实现 http.ResponseWriter,终端和
// 中间件可直接写响应,状态追踪统一在 Ctx 内。
// Ctx is the gin/echo-style execution context: a pooled concrete type passed
// directly from request to handler with zero interface dispatch. Ctx
// implements http.ResponseWriter; the terminal and middleware write responses
// directly, with all state tracking unified inside Ctx.
package ghttp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"sync"
)

// HandlerFunc 是执行链的基本单元:中间件与终端同型,终端不调用 Next。
// HandlerFunc is the execution chain's basic unit: middleware and terminal
// share the type; the terminal never calls Next.
type HandlerFunc func(*Ctx)

// Ctx 承载一次请求的全部状态。Ctx 实现 http.ResponseWriter;终端和
// 中间件通过 c.W 访问底层 writer,或直接对 c 调用 Write/WriteHeader。
// Ctx carries all state for one request. Ctx implements http.ResponseWriter;
// the terminal and middleware access the underlying writer via c.W or call
// Write/WriteHeader on c directly.
type Ctx struct {
	W http.ResponseWriter
	R *http.Request

	// params 由路由匹配阶段一次性填充(paramSlots 槽位序)。
	// params is filled once by route matching in paramSlots slot order.
	params pathParamList

	// handlers 是注册期固化的切片链;index 是 Next 的游标。
	// handlers is the registration-frozen slice chain; index is Next's cursor.
	handlers []HandlerFunc
	index    int

	// body 懒 memo:单次读零缓冲,重复读重放。
	// body is lazily memoized: a single read passes through unbuffered,
	// repeated reads replay.
	body *memoBody

	status int

	// store 是请求级临时状态(Set/Get),懒分配。
	// store is the per-request scratch state (Set/Get), allocated lazily.
	store *store

	// server 是所属 Server 引用,dispatch 时填充,供 writeError 等使用。
	// server is the owning Server reference, set at dispatch, used by
	// writeError and similar helpers.
	server *Server

	// --- 响应状态追踪(合并原 responseWriteState) ---
	// response state tracking (merged from the old responseWriteState)

	committed    bool
	suppressBody bool
	errorHandled bool

	// --- 表单缓存(合并原 requestState.form) ---
	// form cache (merged from the old requestState.form)

	form            map[string][]string
	postForm        map[string][]string
	formErr         error
	formParsed      bool
	multipart       *multipart.Form
	multipartErr    error
	multipartParsed bool

	// --- 惰性路径参数(合并原 requestState.matched) ---
	// lazy path params (merged from the old requestState.matched)

	lazyParams *lazyPathParams

	// --- Params 视图共享(合并原 requestState.params) ---
	// shared Params view (merged from the old requestState.params)

	paramsState paramsState
	paramsBuilt bool

	// --- body 限制(合并原 maxBodyBytes 注入) ---
	// body limit (merged from the old maxBodyBytes injection)

	bodyLimit int64
}

// --- http.ResponseWriter 实现 ---

func (c *Ctx) Header() http.Header    { return c.W.Header() }
func (c *Ctx) WriteHeader(status int) { c.status = status; c.committed = true; c.W.WriteHeader(status) }
func (c *Ctx) Write(data []byte) (int, error) {
	if c.suppressBody {
		return len(data), nil
	}
	if !c.committed {
		c.committed = true
		if c.status == 0 {
			c.status = http.StatusOK
		}
	}
	return c.W.Write(data)
}

// 可选接口透传
// optional interface passthrough

// WriteString 实现 io.StringWriter;下游支持时避免字符串 → []byte 分配。
// WriteString implements io.StringWriter; avoids string→[]byte allocation
// when the underlying writer supports it.
func (c *Ctx) WriteString(s string) (int, error) {
	if c.suppressBody {
		return len(s), nil
	}
	if !c.committed {
		c.committed = true
		if c.status == 0 {
			c.status = http.StatusOK
		}
	}
	if sw, ok := c.W.(io.StringWriter); ok {
		return sw.WriteString(s)
	}
	return c.W.Write([]byte(s))
}

func (c *Ctx) Flush() {
	if f, ok := c.W.(http.Flusher); ok {
		f.Flush()
	}
}
func (c *Ctx) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := c.W.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying writer does not support hijacking")
}

// --- ctxKey:把 Ctx 放入请求 context 的键(替代旧 installState) ---

type ctxKey struct{}

// ctxFromRequest 从请求提取 Ctx;未注入返回 nil。
// ctxFromRequest retrieves the Ctx from the request; nil when not injected.
func ctxFromRequest(r *http.Request) *Ctx {
	if r == nil {
		return nil
	}
	c, _ := r.Context().Value(ctxKey{}).(*Ctx)
	return c
}

// --- 池 ---

var ctxPool = sync.Pool{New: func() any { return new(Ctx) }}

func acquireCtx(w http.ResponseWriter, r *http.Request) *Ctx {
	c := ctxPool.Get().(*Ctx)
	c.reset(w, r)
	return c
}

func (c *Ctx) reset(w http.ResponseWriter, r *http.Request) {
	c.W = w
	c.R = r
	c.params.Reset()
	c.index = 0
	c.status = 0
	c.committed = false
	c.suppressBody = false
	c.errorHandled = false
	c.formErr = nil
	c.formParsed = false
	c.multipartErr = nil
	c.multipartParsed = false
	c.paramsBuilt = false
	c.bodyLimit = 0
}

func releaseCtx(c *Ctx) {
	c.body = nil
	c.handlers = nil
	c.store = nil
	c.form = nil
	c.postForm = nil
	c.multipart = nil
	c.lazyParams = nil
	c.R = nil
	c.W = nil
	c.server = nil
	c.paramsState = paramsState{}
	ctxPool.Put(c)
}

// Next 沿切片链推进;中间件在每个逻辑点调用一次。
// Next advances along the slice chain; middleware calls it once per logical
// point it wants to proceed past.
func (c *Ctx) Next() {
	for c.index < len(c.handlers) {
		h := c.handlers[c.index]
		mark := c.index
		c.index++
		h(c)
		// 中间件未调 Next(或链已走完)即短路:index 只前进了一格。
		// a middleware that did not call Next (or a finished chain) aborts:
		// the index advanced by exactly one slot.
		if c.index <= mark+1 {
			break
		}
	}
}

// Abort 显式终止剩余链(gin 风格);不调 Next 也会自动短路(echo 风格)。
// Abort explicitly stops the remaining chain (gin style); returning without
// calling Next also aborts automatically (echo style).
func (c *Ctx) Abort() {
	c.index = len(c.handlers)
}

// Param 按名字取路径参数。链上 O(n) 扫描(参数 ≤ 若干,实测比 map 快)。
// Param returns a path param by name with an O(n) scan (params are few;
// measured faster than a map for these sizes).
func (c *Ctx) Param(name string) string {
	return c.params.Get(name)
}

// Params 返回匹配期填充的全部路径参数(只读视图)。
// Params returns all match-filled path params (read-only view).
func (c *Ctx) Params() pathParamList {
	return c.params
}

// Body 返回懒 memo 的请求体:首次 Read 直传底层流,重复读取可重放。
// Body returns the lazily memoized request body: the first Read passes
// through the underlying stream, repeated reads replay.
func (c *Ctx) Body() io.ReadCloser {
	return c.memoizedBody()
}

func (c *Ctx) memoizedBody() *memoBody {
	if c.body == nil && c.R != nil && c.R.Body != nil {
		c.body = &memoBody{src: c.R.Body, limit: c.bodyLimit}
		c.R.Body = c.body
	}
	return c.body
}

// BodyBytes 读完整请求体并返回字节(懒 memo,重复调用零成本)。
// BodyBytes reads the full request body once and returns the bytes
// (lazily memoized; repeat calls are free).
func (c *Ctx) BodyBytes() ([]byte, error) {
	return c.memoizedBody().bytes()
}

// Status 设置响应状态码(默认 200)。
// Status sets the response status code (default 200).
func (c *Ctx) Status(code int) {
	c.status = code
}

// Context 返回请求的 context。
// Context returns the request's context.
func (c *Ctx) Context() context.Context {
	return c.R.Context()
}

// store 是中间件/终端共享的每请求临时状态容器,Set/Get 时懒分配。
// store is the per-request scratch state shared by middleware and terminal,
// allocated lazily on first Set/Get.
type store struct {
	mu sync.Mutex
	m  map[string]any
}

// Set 存入一个命名值;下游用 GetValue[T] 类型安全取回。
// Set stores a named value; downstream code retrieves it type-safely with
// GetValue[T].
func (c *Ctx) Set(key string, value any) {
	if c.store == nil {
		c.store = &store{}
	}
	c.store.mu.Lock()
	if c.store.m == nil {
		c.store.m = make(map[string]any, 4)
	}
	c.store.m[key] = value
	c.store.mu.Unlock()
}

// Get 按 key 取回 Set 存入的值;键不存在返回 (nil, false)。
// 类型安全取回用包级函数 GetValue[T](c, key)。
// Get retrieves a stored value by key; missing key returns (nil, false).
// Use the package-level GetValue[T](c, key) for type-safe retrieval.
func (c *Ctx) Get(key string) (any, bool) {
	if c.store == nil || c.store.m == nil {
		return nil, false
	}
	c.store.mu.Lock()
	value, ok := c.store.m[key]
	c.store.mu.Unlock()
	return value, ok
}

// GetValue 按 key 取回 Set 存入的值,编译期断言类型 T;键不存在或类型不符
// 返回零值。
// GetValue retrieves a stored value by key with compile-time type T; a
// missing key or type mismatch returns the zero value.
func GetValue[T any](c *Ctx, key string) (T, bool) {
	var zero T
	value, ok := c.Get(key)
	if !ok {
		return zero, false
	}
	typed, ok := value.(T)
	return typed, ok
}

// --- 表单缓存 ---

// formValues 首次调用时解析表单并缓存 (merged, post) 两套视图。字节未被
// 整读时走 stdlib 解析(流经 memo 时被记录);已被 RawBody/中间件整读时从
// 共享字节回填并回写 stdlib 视图。语义与原 formValuesFromRequest 一致。
// formValues parses the form on first call and caches both views (merged,
// post). When the stream is not yet drained it uses stdlib parsing (bytes are
// recorded via the memo); when already drained by RawBody or middleware it
// backfills from the shared bytes and restores the stdlib view. Semantics
// match the old formValuesFromRequest.
func (c *Ctx) formValues() (url.Values, url.Values, error) {
	if c.formParsed {
		return c.form, c.postForm, c.formErr
	}
	c.formParsed = true
	req := c.R
	// 字节尚未被整读:走 stdlib 解析,流经 memo 时被记录。
	// stream not yet drained: use stdlib parsing; bytes are recorded via memo.
	if c.body == nil || !c.body.drained() {
		// 先确保 memo 存在并接管 req.Body:stdlib ParseForm 读 memo,
		// 字节被记录,RawBody 后续可重放(与旧 installState 语义一致)。
		// ensure the memo exists and takes over req.Body first: stdlib
		// ParseForm reads the memo, the bytes get recorded, and RawBody
		// can replay them later (old installState parity).
		c.memoizedBody()
		if c.bodyLimit > 0 {
			req.Body = http.MaxBytesReader(c.W, req.Body, c.bodyLimit)
		}
		if err := req.ParseForm(); err != nil {
			c.formErr = err
			return c.form, c.postForm, c.formErr
		}
		c.form, c.postForm = req.Form, req.PostForm
		return c.form, c.postForm, nil
	}
	// 已被 RawBody/middleware 整读:从共享字节回填,并回写 stdlib 视图。
	// already drained: backfill from shared bytes and restore the stdlib view.
	raw, err := c.body.bytes()
	if err != nil {
		c.formErr = err
		return c.form, c.postForm, c.formErr
	}
	post, perr := url.ParseQuery(string(raw))
	if perr != nil {
		c.formErr = perr
		return c.form, c.postForm, c.formErr
	}
	form := mergeURLValues(req.URL.Query(), post)
	c.form, c.postForm = form, post
	req.PostForm = post
	req.Form = form
	return c.form, c.postForm, nil
}

// multipartForm 首次调用时解析 multipart 表单并缓存。语义与原
// MultipartForm 一致(仅状态来源从 requestState 迁移到 Ctx)。
// multipartForm parses the multipart form on first call and caches it.
// Semantics match the old MultipartForm (state source migrated to Ctx).
func (c *Ctx) multipartForm(maxMemory int64) (*multipart.Form, error) {
	if c.multipartParsed {
		return c.multipart, c.multipartErr
	}
	c.multipartParsed = true
	req := c.R
	if maxMemory <= 0 {
		maxMemory = defaultMaxMemory
	}
	if c.body != nil && c.body.drained() {
		c.multipartErr = fmt.Errorf("%w: multipart body must be parsed before the raw bytes are drained", ErrInvalidBody)
		return nil, c.multipartErr
	}
	if c.bodyLimit > 0 && c.body == nil {
		req.Body = http.MaxBytesReader(c.W, req.Body, c.bodyLimit)
	}
	if err := req.ParseMultipartForm(maxMemory); err != nil {
		c.multipartErr = err
		return nil, c.multipartErr
	}
	c.multipart = req.MultipartForm
	if c.body != nil {
		if err := c.body.checkLimit(); err != nil {
			c.multipartErr = err
		}
	}
	return c.multipart, c.multipartErr
}

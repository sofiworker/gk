package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	internalcodec "github.com/sofiworker/gk/ghttp/internal/codec"
)

// 本文件是 client 的执行引擎：把 *Request 落实为 *http.Request，跑中间件链，发送，
// 读取响应，判定状态并映射错误。它是本包唯一产生网络行为的地方。
// This file is the client execution engine: it materializes a *Request into an
// *http.Request, runs the middleware chains, sends, reads the response, decides the
// status and maps errors. It is the only place in this package that produces network
// activity.

// rebuildHandlers 在配置变更后重新折叠中间件链。折叠发生在配置期而非请求期：请求期只做
// 一次函数调用，不迭代、不组装、不分配。洋葱序与 ghttp.Server 一致——先注册的最外层、
// 最先执行。
// rebuildHandlers re-foldsthe middleware chains after a configuration change. Folding
// happens at configuration time, not per request: a request makes one function call and
// iterates, assembles and allocates nothing. The onion order matches ghttp.Server —
// first registered is outermost and runs first.
func (c *Client) rebuildHandlers() {
	h := Handler(c.sendWithRetry)
	for i := len(c.reqMiddleware) - 1; i >= 0; i-- {
		h = c.reqMiddleware[i](h)
	}
	c.handler = h

	rh := ResponseHandler(func(_ context.Context, resp *Response) (*Response, error) { return resp, nil })
	for i := len(c.respMiddleware) - 1; i >= 0; i-- {
		rh = c.respMiddleware[i](rh)
	}
	c.respHandler = rh
}

// do 执行一次完整请求：前置观察 → 请求中间件链（含发送）→ 响应中间件链 → 终态判定。
// do runs one full request: pre-observation, the request middleware chain (including
// the send), the response middleware chain, and the terminal decision.
func (c *Client) do(r *Request) (*Response, error) {
	if r.err != nil {
		return nil, r.err
	}
	ctx := r.Context()
	if r.timeoutSet && r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	defer func() {
		if rec := recover(); rec != nil {
			for _, h := range c.panicHooks {
				h(ctx, rec)
			}
			// 只观察，不吞掉：panic 继续向上传播，与 server 的 Recovery"显式选择才恢复"一致。
			// Observe only, never swallow: the panic keeps propagating, matching the
			// server's opt-in Recovery.
			panic(rec)
		}
	}()

	for _, h := range c.beforeRequest {
		if err := h(ctx, r); err != nil {
			return nil, err
		}
	}

	resp, err := c.handler(ctx, r)
	if err != nil {
		// 传输层或中间件失败。http.Client 在 CheckRedirect 返回错误时会给到
		// (resp != nil, err != nil) 这一唯一组合，此时 body 已被标准库关闭，但仍需按
		// 未关闭处理以免重复 Close 造成困惑；这里统一交给 finishFailure 收尾。
		// Transport/middleware failure. http.Client yields (resp != nil, err != nil) in
		// exactly one case — CheckRedirect returning an error — where the standard
		// library already closed the body; finishFailure handles both shapes.
		return c.finishFailure(ctx, r, resp, err)
	}

	if resp, err = c.respHandler(ctx, resp); err != nil {
		return c.finishFailure(ctx, r, resp, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("ghttp/client: middleware returned a nil response")
	}

	for _, h := range c.afterResponse {
		if herr := h(ctx, resp); herr != nil {
			return c.finishFailure(ctx, r, resp, herr)
		}
	}

	return c.finish(ctx, r, resp)
}

// finish 做终态判定：状态码不在可接受集合内时报 *Error；成功且注册了 SetResult 时解码响应体。
// finish makes the terminal decision: a status outside the accepted set yields an
// *Error; a success with SetResult registered decodes the body.
func (c *Client) finish(ctx context.Context, r *Request, resp *Response) (*Response, error) {
	if !c.accepts(resp.status) {
		e := c.newStatusError(r, resp)
		// 结构化错误体：先让调用方注册的目标解一次，再交给自定义解码器收尾。
		// 两者失败都不覆盖状态错误本身——它才是调用方最需要的信息。
		// Structured error body: decode into the caller's registered target first, then
		// let a custom decoder finish. Failures here never replace the status error,
		// which is the information the caller needs most.
		if r.errTgt != nil {
			if derr := resp.Decode(r.errTgt); derr != nil {
				e.Err = errors.Join(e.Err, derr)
			}
		}
		if c.errorDecoder != nil {
			if derr := c.errorDecoder(resp, e); derr != nil {
				e.Err = errors.Join(e.Err, derr)
			}
		}
		c.emitError(ctx, e, resp)
		return resp, e
	}

	if r.result != nil {
		if err := resp.Decode(r.result); err != nil {
			e := c.newStatusError(r, resp)
			e.Err = err
			c.emitError(ctx, e, resp)
			return resp, e
		}
	}
	for _, h := range c.successHooks {
		h(ctx, resp)
	}
	return resp, nil
}

// accepts 报告状态码是否可接受，并纳入"禁止重定向 ⇒ 3xx 可接受"这一组合语义。
// accepts reports whether a status is acceptable, including the combined semantics of
// "redirects disabled ⇒ 3xx acceptable".
func (c *Client) accepts(status int) bool {
	if c.acceptedStatus(status) {
		return true
	}
	return c.acceptRedirects && status >= 300 && status < 400
}

// finishFailure 把"没走到终态"的失败统一成错误并触发失败钩子。它保证流式模式下已打开的
// 连接被关闭，避免中间件在拿到响应后又返回错误时泄漏连接。
// finishFailure turns a failure that never reached the terminal decision into an error
// and fires the failure hooks. It guarantees that an already-opened stream is closed,
// so middleware that returns an error after receiving a response cannot leak a
// connection.
func (c *Client) finishFailure(ctx context.Context, r *Request, resp *Response, err error) (*Response, error) {
	if resp != nil && resp.IsStream() {
		_ = resp.Close()
	}
	e := c.newStatusError(r, resp)
	if resp == nil {
		e.StatusCode = 0
		e.Status = ""
	}
	e.Err = err
	c.emitError(ctx, e, resp)
	return resp, e
}

// newStatusError 构造承载本次请求元信息的 *Error。
// newStatusError builds the *Error carrying this request's metadata.
func (c *Client) newStatusError(r *Request, resp *Response) *Error {
	e := &Error{
		Op:       methodOp(r.method),
		Method:   strings.ToUpper(r.method),
		Attempts: 1,
		Response: resp,
	}
	if resp != nil {
		e.URL = respURL(resp, r)
		e.StatusCode = resp.status
		e.Status = resp.Status()
		e.Attempts = resp.attempts
		return e
	}
	if raw, err := c.buildURL(r); err == nil {
		e.URL = raw
	}
	return e
}

// emitError 触发失败钩子。
// emitError fires the failure hooks.
func (c *Client) emitError(ctx context.Context, e *Error, resp *Response) {
	for _, h := range c.errorHooks {
		h(ctx, e, resp)
	}
}

// buildRequest 把 *Request 落实为一个可发送的 *http.Request。
// buildRequest materializes a *Request into a sendable *http.Request.
func (c *Client) buildRequest(ctx context.Context, r *Request) (*http.Request, error) {
	if strings.TrimSpace(r.method) == "" {
		return nil, fmt.Errorf("ghttp/client: method is empty")
	}
	full, err := c.buildURL(r)
	if err != nil {
		return nil, err
	}
	body, contentType, err := c.encodeBody(r)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, strings.ToUpper(r.method), full, body)
	if err != nil {
		return nil, fmt.Errorf("ghttp/client: build request: %w", err)
	}
	// 先铺 Client 默认头，再让请求头覆盖；请求级设置始终优先。
	// Lay down the client defaults, then let request headers override: request-level
	// settings always win.
	for k, vs := range c.header {
		httpReq.Header[k] = append([]string(nil), vs...)
	}
	for k, vs := range r.header {
		httpReq.Header[k] = append([]string(nil), vs...)
	}
	if contentType != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if httpReq.Header.Get("User-Agent") == "" && c.userAgent != "" {
		httpReq.Header.Set("User-Agent", c.userAgent)
	}
	if r.bodyLengthSet {
		httpReq.ContentLength = r.bodyLength
	}
	c.applyAuth(httpReq, r)
	return httpReq, nil
}

// buildURL 解析路径参数、拼接 BaseURL、附加查询参数，得到最终 URL。
// buildURL resolves path parameters, joins the BaseURL and appends query values to
// produce the final URL.
func (c *Client) buildURL(r *Request) (string, error) {
	rawURL, err := applyPathParams(r.rawURL, r.path)
	if err != nil {
		return "", err
	}
	return resolveURL(c.baseURL, rawURL, r.query)
}

// encodeBody 决定请求体的字节来源：显式 reader 优先，否则按 Content-Type 选编解码器编码。
//
// 内存载体（*bytes.Buffer）会被 http.NewRequest 自动识别并填好 ContentLength 与
// GetBody，这正是重试与 307/308 重定向能重放请求体的前提；io.Reader 型 body 没有这个
// 待遇，其可重放性由重试层在发送前判定。
// encodeBody decides the body's byte source: an explicit reader wins, otherwise the
// codec selected by Content-Type encodes it.
//
// An in-memory carrier (*bytes.Buffer) is recognized by http.NewRequest, which fills in
// ContentLength and GetBody — exactly the precondition for replaying a body on retries
// and 307/308 redirects. An io.Reader body gets none of that; the retry layer decides
// its replayability before sending.
func (c *Client) encodeBody(r *Request) (io.Reader, string, error) {
	if r.isMultipart {
		return r.encodeMultipart()
	}
	if r.bodyReader != nil {
		return r.bodyReader, r.bodyContentType, nil
	}
	if r.body == nil {
		return nil, "", nil
	}
	ct := r.bodyContentType
	if ct == "" {
		ct = internalcodec.ContentTypeJSON
	}
	cd, ok := lookupCodec(c.codecs, ct)
	if !ok || cd == nil {
		return nil, "", fmt.Errorf("%w: %s", ErrNoCodec, ct)
	}
	buf := new(bytes.Buffer)
	if err := cd.Encode(buf, r.body); err != nil {
		return nil, "", fmt.Errorf("ghttp/client: encode request body: %w", err)
	}
	return buf, ct, nil
}

// applyAuth 按优先级写认证头：Basic 优先于 token；两者都未设置时不写。
// applyAuth writes the credentials header by precedence: Basic beats a token; neither
// set means nothing is written.
func (c *Client) applyAuth(httpReq *http.Request, r *Request) {
	switch {
	case r.hasBasicAuth:
		httpReq.SetBasicAuth(r.basicUser, r.basicPass)
	case r.authToken != "":
		scheme := r.authScheme
		if scheme == "" {
			scheme = "Bearer"
		}
		key := r.authKey
		if key == "" {
			key = c.authKey
		}
		if key == "" {
			key = "Authorization"
		}
		httpReq.Header.Set(key, scheme+" "+r.authToken)
	}
}

// readResponse 依据本请求的响应体策略把 *http.Response 收敛成 *Response：
// 落盘、流式、或受上限保护地读入内存。
// readResponse collapses an *http.Response into a *Response per this request's body
// policy: write to file, stream, or read into memory under a size cap.
func (c *Client) readResponse(r *Request, raw *http.Response, elapsed time.Duration) (*Response, error) {
	resp := &Response{
		request:  r,
		raw:      raw,
		status:   raw.StatusCode,
		header:   raw.Header,
		duration: elapsed,
		attempts: 1,
		result:   r.result,
		errTgt:   r.errTgt,
	}
	switch {
	case r.outputFile != "":
		if err := resp.SaveToFile(r.outputFile); err != nil {
			return resp, err
		}
	case r.stream:
		resp.bodyStream = raw.Body
	default:
		limit := c.responseBodyLimit
		if r.limitSet {
			limit = r.responseBodyLimit
		}
		data, err := readAllLimited(raw.Body, limit)
		closeErr := raw.Body.Close()
		if err != nil {
			return resp, err
		}
		if closeErr != nil {
			return resp, closeErr
		}
		resp.body = data
	}
	return resp, nil
}

// DoHTTP 用本 Client 的编排（中间件、Cookie Jar、重定向策略、超时）发送一个标准库
// *http.Request，并返回统一形态的 *Response。响应体按 Client 默认策略读入内存。
//
// 它是"已有标准库请求"接入本包编排层的入口；反向的降级入口是 Request.HTTPRequest。
// DoHTTP sends a standard-library *http.Request through this Client's orchestration
// (middleware, cookie jar, redirect policy, timeouts) and returns a unified *Response.
// The body is read into memory per the Client default policy.
//
// It is the entry point for feeding an existing standard-library request into this
// package's orchestration; Request.HTTPRequest is the reverse escape hatch.
func (c *Client) DoHTTP(ctx context.Context, httpReq *http.Request) (*Response, error) {
	r := c.R().SetContext(ctx).SetMethod(httpReq.Method)
	r.rawURL = httpReq.URL.String()
	if httpReq.Body != nil {
		r.bodyReader = httpReq.Body
		r.body = nil
		r.bodyContentType = httpReq.Header.Get("Content-Type")
		if httpReq.ContentLength > 0 {
			r.bodyLength = httpReq.ContentLength
			r.bodyLengthSet = true
		}
	}
	for k, vs := range httpReq.Header {
		r.header[k] = append([]string(nil), vs...)
	}
	return r.Send()
}

// HTTPRequest 把本请求落实为标准库 *http.Request，供外部直接用 http.Client 发送，
// 或塞进需要 *http.Request 的组件。请求体在这一刻被编码，因此返回后修改 Request 不再
// 影响已生成的 *http.Request。
// HTTPRequest materializes this request into a standard-library *http.Request so an
// outside http.Client can send it, or so it can be handed to any component expecting
// one. The body is encoded at this moment, so later mutations of the Request no longer
// affect the produced *http.Request.
func (r *Request) HTTPRequest(ctx context.Context) (*http.Request, error) {
	if r.err != nil {
		return nil, r.err
	}
	if ctx == nil {
		ctx = r.Context()
	}
	return r.client.buildRequest(ctx, r)
}

// methodOp 把方法名转成适合日志的前缀形式（"GET" → "Get"）。
// methodOp turns a method name into a log-friendly prefix ("GET" → "Get").
func methodOp(method string) string {
	if method == "" {
		return ""
	}
	upper := strings.ToUpper(method)
	return upper[:1] + strings.ToLower(upper[1:])
}

// respURL 取响应对应的最终 URL（含重定向后的地址），取不到时回退到请求侧的解析结果。
// respURL returns the response's final URL (after redirects), falling back to the
// request-side resolution.
func respURL(resp *Response, r *Request) string {
	if resp != nil && resp.raw != nil && resp.raw.Request != nil && resp.raw.Request.URL != nil {
		return resp.raw.Request.URL.String()
	}
	if resp != nil && resp.request != nil && resp.request.client != nil {
		if u, err := resp.request.client.buildURL(resp.request); err == nil {
			return u
		}
	}
	return r.rawURL
}

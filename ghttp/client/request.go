package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Request 是一个可变、fluent 的请求构造器，由 Client.R() 派生。它【不】并发安全：
// 约定在一次请求内构造完立即终结（Send/Get/Post/…），不要跨 goroutine 共享。
//
// 错误契约（链式 setter 的结构性问题）：SetXxx 无法返回 error，因此凡能立即判定的失败
// 都记录到内部 err 并让后续 setter 空转，终结方法一定把它返回——错误不会丢，只是延迟到
// 终结期暴露。需要提前检查时用 Err()。
// Request is a mutable, fluent request builder derived from Client.R(). It is NOT safe
// for concurrent use: build it, terminate it (Send/Get/Post/…), and discard it within
// one request; never share it across goroutines.
//
// Error contract (the structural problem of chained setters): SetXxx cannot return an
// error, so any failure detectable on the spot is recorded in the internal err and
// later setters become no-ops, with the terminal method guaranteed to return it —
// nothing is lost, only surfaced at termination. Use Err() to check earlier.
type Request struct {
	client *Client
	ctx    context.Context

	method string
	rawURL string
	header http.Header
	query  url.Values
	path   map[string]string

	body            any
	bodyReader      io.Reader
	bodyContentType string
	bodyLength      int64
	bodyLengthSet   bool

	// multipart 状态：isMultipart 为真时请求体由 encodeMultipart 构建，body/bodyReader
	// 被忽略。设置 body 家族会关掉它，设置 multipart 部件会打开它——两者互斥且后设者胜，
	// 避免"同时给了 JSON body 和文件部件"这种无从判断意图的状态。
	// Multipart state: when isMultipart is true the body comes from encodeMultipart and
	// body/bodyReader are ignored. The body setters turn it off and the multipart setters
	// turn it on — mutually exclusive, last writer wins, so there is never an ambiguous
	// "JSON body plus file part" state.
	isMultipart       bool
	formData          url.Values
	multipartFields   []MultipartField
	multipartBoundary string

	result any
	errTgt any

	responseBodyLimit int64
	limitSet          bool
	stream            bool
	outputFile        string

	// timeout 是每请求超时，经 context 生效，不影响 Client 与其他并发请求。
	// timeout is a per-request timeout applied through the context, leaving the Client
	// and other in-flight requests untouched.
	timeout    time.Duration
	timeoutSet bool

	// retry 是本请求的重试策略覆盖；retrySet 为真时优先于 Client 级策略。
	// retry is this request's retry-policy override; retrySet makes it beat the
	// client-level policy.
	retry    RetryPolicy
	retrySet bool

	basicUser    string
	basicPass    string
	hasBasicAuth bool
	authToken    string
	authScheme   string
	authKey      string

	err error
}

// SetContext 设置本次请求的 context。它优先于 Client 级配置，且是取消与超时的正确载体
// （优于 http.Client.Timeout，后者连读 body 一起计时）。
// SetContext sets this request's context. It takes precedence over client-level
// configuration and is the right carrier for cancellation and timeouts (unlike
// http.Client.Timeout, which also times body reads).
func (r *Request) SetContext(ctx context.Context) *Request {
	if ctx == nil {
		r.record(fmt.Errorf("ghttp/client: nil context"))
		return r
	}
	r.ctx = ctx
	return r
}

// SetMethod 设置 HTTP 方法。与 SetURL 配合后用 Send 发出。
// SetMethod sets the HTTP method; pair it with SetURL and finish with Send.
func (r *Request) SetMethod(method string) *Request {
	r.method = method
	return r
}

// SetURL 设置请求 URL：绝对 URL 原样使用；以 '/' 开头或相对形式时与 Client 的 BaseURL 拼接。
// SetURL sets the request URL: an absolute URL is used as-is, while a leading-slash or
// relative form is joined with the Client's BaseURL.
func (r *Request) SetURL(rawURL string) *Request {
	r.rawURL = rawURL
	return r
}

// SetPathParam 设置路径参数，替换 URL 中的 {name} 占位符（值会被转义）。
// SetPathParam sets a path parameter replacing the {name} placeholder in the URL (the
// value is escaped).
func (r *Request) SetPathParam(name, value string) *Request {
	if r.path == nil {
		r.path = map[string]string{}
	}
	r.path[name] = value
	return r
}

// SetPathParams 批量设置路径参数。
// SetPathParams sets several path parameters at once.
func (r *Request) SetPathParams(params map[string]string) *Request {
	for k, v := range params {
		r.SetPathParam(k, v)
	}
	return r
}

// SetHeader 覆盖一个请求头。
// SetHeader overrides a request header.
func (r *Request) SetHeader(key, value string) *Request {
	if r.header == nil {
		r.header = http.Header{}
	}
	r.header.Set(key, value)
	return r
}

// AddHeader 追加一个请求头的值（同名的多个值都保留）。
// AddHeader appends a header value, keeping multiple values of the same name.
func (r *Request) AddHeader(key, value string) *Request {
	if r.header == nil {
		r.header = http.Header{}
	}
	r.header.Add(key, value)
	return r
}

// SetHeaders 覆盖多个请求头。
// SetHeaders overrides several request headers.
func (r *Request) SetHeaders(headers map[string]string) *Request {
	for k, v := range headers {
		r.SetHeader(k, v)
	}
	return r
}

// SetQueryParam 覆盖一个查询参数。值经 queryValue 格式化：标量用 strconv，其余用
// fmt.Sprint，字符串原样；不引入反射。
// SetQueryParam overrides a query parameter. The value is formatted by queryValue:
// scalars via strconv, everything else via fmt.Sprint, strings verbatim — no
// reflection.
func (r *Request) SetQueryParam(key string, value any) *Request {
	if r.query == nil {
		r.query = url.Values{}
	}
	r.query.Set(key, queryValue(value))
	return r
}

// AddQueryParam 追加一个查询参数（保留同名多值，如 ?tag=a&tag=b）。
// AddQueryParam appends a query parameter, keeping repeated keys (?tag=a&tag=b).
func (r *Request) AddQueryParam(key string, value any) *Request {
	if r.query == nil {
		r.query = url.Values{}
	}
	r.query.Add(key, queryValue(value))
	return r
}

// SetQueryParams 批量覆盖查询参数。
// SetQueryParams overrides several query parameters.
func (r *Request) SetQueryParams(params map[string]any) *Request {
	for k, v := range params {
		r.SetQueryParam(k, v)
	}
	return r
}

// SetQueryString 原样附加一段已编码的查询串（不做二次转义），与已设置的参数共存。
// SetQueryString appends an already-encoded query string verbatim (no re-escaping),
// coexisting with the parameters already set.
func (r *Request) SetQueryString(raw string) *Request {
	if r.query == nil {
		r.query = url.Values{}
	}
	r.query.Set(rawQuerySentinel, raw)
	return r
}

// rawQuerySentinel 是 SetQueryString 在 url.Values 中的占位键：它不可能与真实参数名冲突，
// 构建 URL 时被单独摘出来拼接。
// rawQuerySentinel is SetQueryString's placeholder key inside url.Values: it cannot
// collide with a real parameter name and is extracted separately when building the URL.
const rawQuerySentinel = "\x00raw"

// SetBody 设置请求体，编码方式由 Content-Type 决定：显式设置过则用对应的已注册 Codec，
// 否则默认 JSON。需要完全控制时用 SetBodyReader。
// SetBody sets the request body; the encoding follows the Content-Type — the registered
// Codec when one was set explicitly, JSON otherwise. Use SetBodyReader for full control.
func (r *Request) SetBody(v any) *Request {
	r.body = v
	r.isMultipart = false
	return r
}

// SetJSON 以 JSON 编码请求体（并设置 Content-Type: application/json）。
// SetJSON encodes the body as JSON (and sets Content-Type: application/json).
func (r *Request) SetJSON(v any) *Request {
	r.body = v
	r.bodyContentType = mediaTypeOf(JSONCodec().ContentType())
	r.isMultipart = false
	return r
}

// SetXML 以 XML 编码请求体。
// SetXML encodes the body as XML.
func (r *Request) SetXML(v any) *Request {
	r.body = v
	r.bodyContentType = mediaTypeOf(XMLCodec().ContentType())
	r.isMultipart = false
	return r
}

// SetText 以 text/plain 发送字符串或 []byte。
// SetText sends a string or []byte as text/plain.
func (r *Request) SetText(v any) *Request {
	r.body = v
	r.bodyContentType = mediaTypeOf(TextCodec().ContentType())
	r.isMultipart = false
	return r
}

// SetForm 以 application/x-www-form-urlencoded 发送表单。
// SetForm sends a form as application/x-www-form-urlencoded.
func (r *Request) SetForm(values url.Values) *Request {
	r.body = values
	r.bodyContentType = mediaTypeOf(FormCodec().ContentType())
	r.isMultipart = false
	return r
}

// SetBodyReader 直接以流式方式提供请求体，contentType 必须显式给出（无从推断）。
//
// 注意可重放性：io.Reader 型请求体没有 GetBody，无法在重试或 307/308 重定向时重发。
// 若同时开启了重试，本包会在发出第一次请求【之前】以 ErrBodyNotReplayable 拒绝，
// 而不是静默重发一个空 body。需要可重放时请用 SetJSON/SetForm 这类内存载体，
// 或自行传入 io.ReadSeeker 并配合重试。
// SetBodyReader supplies the body as a stream; contentType must be given explicitly
// (there is nothing to infer it from).
//
// Replayability caveat: an io.Reader body has no GetBody, so it cannot be resent on a
// retry or a 307/308 redirect. With retries enabled this package refuses BEFORE the
// first attempt with ErrBodyNotReplayable rather than silently resending an empty
// body. For replayable bodies use an in-memory carrier such as SetJSON/SetForm, or
// pass an io.ReadSeeker and pair it with retries.
func (r *Request) SetBodyReader(rc io.Reader, contentType string) *Request {
	r.bodyReader = rc
	r.body = nil
	r.bodyContentType = mediaTypeOf(contentType)
	r.isMultipart = false
	return r
}

// SetContentLength 声明请求体长度。对 io.Reader 型 body 尤其有用：标准库只有在长度已知或
// 类型可推断时才设置 Content-Length，否则退化为 chunked。
// SetContentLength declares the body length. It matters most for io.Reader bodies: the
// standard library only sets Content-Length when the length is known or inferable, and
// otherwise falls back to chunked.
func (r *Request) SetContentLength(n int64) *Request {
	r.bodyLength = n
	r.bodyLengthSet = true
	return r
}

// SetBasicAuth 设置 HTTP Basic 认证凭证。
// SetBasicAuth sets HTTP Basic credentials.
func (r *Request) SetBasicAuth(user, pass string) *Request {
	r.basicUser = user
	r.basicPass = pass
	r.hasBasicAuth = true
	return r
}

// SetAuthToken 设置认证 token，写进 Authorization 头（默认 scheme 为 Bearer，
// 可用 SetAuthScheme 更改）。
// SetAuthToken sets the auth token written into the Authorization header (the default
// scheme is Bearer; change it with SetAuthScheme).
func (r *Request) SetAuthToken(token string) *Request {
	r.authToken = token
	return r
}

// SetAuthScheme 设置认证 scheme（如 "Bearer"、"Token"）。
// SetAuthScheme sets the auth scheme (e.g. "Bearer", "Token").
func (r *Request) SetAuthScheme(scheme string) *Request {
	r.authScheme = scheme
	return r
}

// SetHeaderAuthorizationKey 覆盖承载 token 的头名（每请求生效）。
// SetHeaderAuthorizationKey overrides the header carrying the token, per request.
func (r *Request) SetHeaderAuthorizationKey(key string) *Request {
	r.authKey = key
	return r
}

// SetResult 注册响应体的解码目标（须为指针）。这是运行时才知道目标类型的唯一路径，
// 泛型入口则用 Into[T]（见 generic.go）。
// SetResult registers the decode target for the response body (must be a pointer). This
// is the only path when the target type is known only at runtime; generic entries use
// Into[T] instead (see generic.go).
func (r *Request) SetResult(v any) *Request {
	r.result = v
	return r
}

// SetError 注册结构化错误体的解码目标（须为指针），仅在状态码不可接受时使用。
// SetError registers the decode target for a structured error body (must be a
// pointer); it is used only when the status is unacceptable.
func (r *Request) SetError(v any) *Request {
	r.errTgt = v
	return r
}

// SetTimeout 设置本请求的独立超时。它通过 context 实现，因此不会改动 Client，也不影响
// 并发中的其他请求；与 http.Client.Timeout 不同，它可以只覆盖本次调用。
// SetTimeout sets an independent timeout for this request. It is implemented via the
// context, so it neither mutates the Client nor affects concurrent requests; unlike
// http.Client.Timeout it scopes to this single call.
func (r *Request) SetTimeout(d time.Duration) *Request {
	r.timeout = d
	r.timeoutSet = true
	return r
}

// SetResponseBodyLimit 覆盖本请求的响应体内存上限。
// SetResponseBodyLimit overrides this request's in-memory response-body cap.
func (r *Request) SetResponseBodyLimit(n int64) *Request {
	if n > 0 {
		r.responseBodyLimit = n
		r.limitSet = true
	}
	return r
}

// SetUnlimitedResponseBody 本请求解除响应体上限。
// SetUnlimitedResponseBody lifts the cap for this request.
func (r *Request) SetUnlimitedResponseBody() *Request {
	r.responseBodyLimit = 0
	r.limitSet = true
	return r
}

// SetStreamResponse 让本请求以流式模式返回：Response.Body 持有未读的 io.ReadCloser，
// Decode 直接流式解码，而 Bytes/String 会返回 ErrStreamConsumed。调用方必须 Close。
// SetStreamResponse makes this request stream: Response.Body holds an unread
// io.ReadCloser, Decode streams from it, and Bytes/String return ErrStreamConsumed. The
// caller must Close.
func (r *Request) SetStreamResponse() *Request {
	r.stream = true
	return r
}

// SetOutputFile 让响应体直接落盘到 path（内存中不保留副本），适合下载。
// SetOutputFile streams the response body straight into path (no in-memory copy),
// suited to downloads.
func (r *Request) SetOutputFile(path string) *Request {
	r.outputFile = path
	return r
}

// Err 返回链上累积的首个错误；终结方法（Send/Get/…）一定会把它返回。链中主动检查用它。
// Err returns the first error accumulated on the chain; terminal methods (Send/Get/…)
// always return it. Use it to check earlier inside the chain.
func (r *Request) Err() error { return r.err }

// record 记录链上首个错误；已有错误时保持首个（最接近根因）。
// record keeps the first error on the chain (closest to the root cause).
func (r *Request) record(err error) {
	if err != nil && r.err == nil {
		r.err = err
	}
}

// Method 返回已设置的方法（供中间件与调试读取）。
// Method returns the configured method (read by middleware and debugging).
func (r *Request) Method() string { return r.method }

// URL 返回已设置的原始 URL（未经 base 拼接与转义）。
// URL returns the raw URL as set (before base joining and escaping).
func (r *Request) URL() string { return r.rawURL }

// Header 返回请求头集合（可变，供中间件改写）。
// Header returns the header set (mutable, for middleware to rewrite).
func (r *Request) Header() http.Header { return r.header }

// Context 返回本次请求的 context，未设置时返回 context.Background()。
// Context returns this request's context, or context.Background() when unset.
func (r *Request) Context() context.Context {
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// Client 返回派生本请求的 Client。
// Client returns the Client that derived this request.
func (r *Request) Client() *Client { return r.client }

// ——— 终结方法 ——— //

// Send 用已设置的方法与 URL 发出请求。
// Send issues the request with the configured method and URL.
func (r *Request) Send() (*Response, error) { return r.Execute(r.method, r.rawURL) }

// Execute 用指定的方法与 URL 发出请求，忽略先前设置的同名值。
// Execute issues the request with the given method and URL, ignoring previously set
// values of the same kind.
func (r *Request) Execute(method, rawURL string) (*Response, error) {
	if method != "" {
		r.method = method
	}
	if rawURL != "" {
		r.rawURL = rawURL
	}
	return r.client.do(r)
}

// Get 以 GET 发出请求。
// Get issues the request as GET.
func (r *Request) Get(rawURL string) (*Response, error) { return r.Execute(http.MethodGet, rawURL) }

// Post 以 POST 发出请求。
// Post issues the request as POST.
func (r *Request) Post(rawURL string) (*Response, error) {
	return r.Execute(http.MethodPost, rawURL)
}

// Put 以 PUT 发出请求。
// Put issues the request as PUT.
func (r *Request) Put(rawURL string) (*Response, error) {
	return r.Execute(http.MethodPut, rawURL)
}

// Patch 以 PATCH 发出请求。
// Patch issues the request as PATCH.
func (r *Request) Patch(rawURL string) (*Response, error) {
	return r.Execute(http.MethodPatch, rawURL)
}

// Delete 以 DELETE 发出请求。
// Delete issues the request as DELETE.
func (r *Request) Delete(rawURL string) (*Response, error) {
	return r.Execute(http.MethodDelete, rawURL)
}

// Head 以 HEAD 发出请求。
// Head issues the request as HEAD.
func (r *Request) Head(rawURL string) (*Response, error) {
	return r.Execute(http.MethodHead, rawURL)
}

// Options 以 OPTIONS 发出请求。
// Options issues the request as OPTIONS.
func (r *Request) Options(rawURL string) (*Response, error) {
	return r.Execute(http.MethodOptions, rawURL)
}

// ——— 内部工具 ——— //

// queryValue 把查询参数值格式化为字符串，不引入反射：字符串与 []byte 原样，
// 标量走 strconv，其余交给 fmt.Sprint。
// queryValue formats a query value without reflection: strings and []byte verbatim,
// scalars via strconv, everything else via fmt.Sprint.
func queryValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case []byte:
		return string(val)
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int8:
		return strconv.FormatInt(int64(val), 10)
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint8:
		return strconv.FormatUint(uint64(val), 10)
	case uint16:
		return strconv.FormatUint(uint64(val), 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float32:
		return strconv.FormatFloat(float64(val), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'g', -1, 64)
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprint(v)
	}
}

// applyPathParams 把 URL 中的 {name} 占位符替换为已设置的值（值经 url.PathEscape 转义）。
// 存在未提供值的占位符时报错，而不是把一个含花括号的 URL 发出去——那种请求只会得到
// 服务端 404，掩盖了真正的接线错误。
// applyPathParams replaces {name} placeholders with the configured values (escaped via
// url.PathEscape). A placeholder without a value is an error rather than sending a URL
// containing braces — such a request only earns a 404 and hides the real wiring bug.
func applyPathParams(rawURL string, params map[string]string) (string, error) {
	if !strings.ContainsRune(rawURL, '{') {
		return rawURL, nil
	}
	var b strings.Builder
	b.Grow(len(rawURL))
	for i := 0; i < len(rawURL); {
		if rawURL[i] != '{' {
			b.WriteByte(rawURL[i])
			i++
			continue
		}
		end := strings.IndexByte(rawURL[i:], '}')
		if end < 0 {
			b.WriteString(rawURL[i:])
			break
		}
		name := rawURL[i+1 : i+end]
		value, ok := params[name]
		if !ok {
			return "", fmt.Errorf("ghttp/client: path parameter %q has no value", name)
		}
		b.WriteString(url.PathEscape(value))
		i += end + 1
	}
	return b.String(), nil
}

// resolveURL 把请求 URL 与 Client 的 BaseURL 拼成绝对 URL，并附加查询参数。
// resolveURL joins the request URL with the Client's BaseURL and appends query values.
func resolveURL(baseURL, rawURL string, query url.Values) (string, error) {
	full := rawURL
	if !isAbsoluteURL(full) {
		if baseURL == "" {
			return "", ErrNoBaseURL
		}
		full = strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(full, "/")
	}
	if len(query) == 0 {
		return full, nil
	}
	u, err := url.Parse(full)
	if err != nil {
		return "", fmt.Errorf("ghttp/client: invalid URL %q: %w", full, err)
	}
	raw := query.Get(rawQuerySentinel)
	q := u.Query()
	for k, vs := range query {
		if k == rawQuerySentinel {
			continue
		}
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	encoded := q.Encode()
	if raw != "" {
		if encoded != "" {
			encoded += "&" + raw
		} else {
			encoded = raw
		}
	}
	u.RawQuery = encoded
	return u.String(), nil
}

// isAbsoluteURL 报告 s 是否已带 scheme（http/https 等），即不需要拼接 BaseURL。
// isAbsoluteURL reports whether s already carries a scheme and needs no BaseURL join.
func isAbsoluteURL(s string) bool {
	i := strings.Index(s, "://")
	if i <= 0 {
		return false
	}
	for _, c := range s[:i] {
		if !isSchemeChar(c) {
			return false
		}
	}
	return true
}

// isSchemeChar 报告 c 是否是 RFC 3986 scheme 允许的字符（ALPHA / DIGIT / "+" / "-" / "."）。
// isSchemeChar reports whether c is allowed in an RFC 3986 scheme (ALPHA / DIGIT / "+"
// / "-" / ".").
func isSchemeChar(c rune) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '+' || c == '-' || c == '.':
		return true
	default:
		return false
	}
}

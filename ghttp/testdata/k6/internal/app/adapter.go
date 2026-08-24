package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/sofiworker/gk/gerr"
	"github.com/sofiworker/gk/ghttp"
)

// ===========================================================================
// 适配器与公共辅助 / Adapters and shared helpers
// ===========================================================================

// rawAdapter 将 http.HandlerFunc 包装为 ghttp.RawHandlerFunc,使现有 handler 逻辑
// 以最小改动兼容 ghttp 的原生签名。传入的 handler 直接 access resp(ghttp.Response
// 实现 http.ResponseWriter) 与 req.Request(*http.Request)。
// rawAdapter wraps an http.HandlerFunc as a ghttp.RawHandlerFunc so existing
// handler logic can be reused with the ghttp native signature with minimal
// changes. When called, the handler directly accesses resp (ghttp.Response
// implements http.ResponseWriter) and req.Request (*http.Request).
func rawAdapter(h http.HandlerFunc) ghttp.RawHandlerFunc {
	return func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		h.ServeHTTP(resp, req.Request)
		return nil
	}
}

// rawAdapterCtx 类似 rawAdapter,但将链 ctx 也传给 handler,供需要 context 的 handler
// (如协作式超时、追踪)使用。
// rawAdapterCtx is like rawAdapter but also passes the chain ctx to the handler,
// for handlers that need context (e.g. cooperative timeout, tracing).
func rawAdapterCtx(h func(context.Context, http.ResponseWriter, *http.Request)) ghttp.RawHandlerFunc {
	return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		h(ctx, resp, req.Request)
		return nil
	}
}

// ===========================================================================
// 错误类型 / Error types
// ===========================================================================

// appHTTPError 封装 HTTP 状态码与消息,同时实现 error 与 ghttp.StatusCoder,使错误链
// 能正确分类。消息字段对外暴露,writePublicError 用于编码为 {code,message} 扁平体。
// appHTTPError wraps an HTTP status code and message, implementing both error and
// ghttp.StatusCoder so the error chain can classify it, and writePublicError
// encodes it to the flat {code,message} body.
type appHTTPError struct {
	code    int
	message string
}

func (e appHTTPError) Error() string   { return e.message }
func (e appHTTPError) HTTPStatus() int { return e.code }

// writePublicError 写扁平 JSON 错误体 {code,message},供端点的错误适配器使用。
// writePublicError writes the flat JSON error body {code,message}, used by the
// endpoint error adapters.
func writePublicError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{Code: status, Message: http.StatusText(status)})
}

// writeProblemError 写 RFC 7807 problem+json 错误体 {status,title,detail},供绑定/
// 编解码路由的失败响应使用。
// writeProblemError writes an RFC 7807 problem+json error body
// {status,title,detail} for binding/codec route failures.
func writeProblemError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}{Type: "about:blank", Status: status, Title: http.StatusText(status), Detail: detail})
}

// ===========================================================================
// 错误分类 / Error classification
// ===========================================================================

var (
	errConflictSentinel    = errors.New("conflict-sentinel")
	errUnavailableSentinel = errors.New("unavailable-sentinel")
)

// classifyError 在应用层面将 error 映射到 HTTP 状态码。
// classifyError maps an error to an HTTP status code at the application level.
func classifyError(err error) int {
	if errors.Is(err, errUnavailableSentinel) {
		return http.StatusServiceUnavailable
	}
	if errors.Is(err, errConflictSentinel) {
		return http.StatusConflict
	}
	if gerrCode, ok := gerrCode(err); ok {
		return gerrCode
	}
	var ae appHTTPError
	if errors.As(err, &ae) {
		return ae.code
	}
	return http.StatusInternalServerError
}

// gerrCode 从 gerr 错误中提取 HTTP 状态码。
// gerrCode extracts an HTTP status code from a gerr error.
func gerrCode(err error) (int, bool) {
	var ge *gerr.Error
	if errors.As(err, &ge) {
		switch ge.Kind {
		case gerr.KindUnavailable:
			return http.StatusServiceUnavailable, true
		case gerr.KindInternal:
			return http.StatusInternalServerError, true
		case gerr.KindInvalid:
			return http.StatusBadRequest, true
		case gerr.KindTimeout:
			return http.StatusGatewayTimeout, true
		case gerr.KindNotFound:
			return http.StatusNotFound, true
		case gerr.KindConflict:
			return http.StatusConflict, true
		case gerr.KindPermission:
			return http.StatusForbidden, true
		}
	}
	return 0, false
}

// ===========================================================================
// 请求计数 / Request counter
// ===========================================================================

type requestCounter struct {
	mu    sync.Mutex
	value uint64
}

func newRequestCounter() *requestCounter { return &requestCounter{} }
func (c *requestCounter) Increment() {
	c.mu.Lock()
	c.value++
	c.mu.Unlock()
}
func (c *requestCounter) Value() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// ===========================================================================
// 公共 / Misc
// ===========================================================================

const testDefaultSecret = "known-k6-secret"

// writeJSON 写 JSON 响应(200,Content-Type: application/json)。
// writeJSON writes a JSON response (200, Content-Type: application/json).
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// responseCanHaveBody 报告特定方法+状态码是否允许响应体。
// responseCanHaveBody reports whether a response body is allowed for the given method and status.
func responseCanHaveBody(method string, status int) bool {
	return method != http.MethodHead && status >= http.StatusOK &&
		status != http.StatusNoContent && status != http.StatusNotModified
}

// decodeJSON 解码请求体为目标类型,失败时写 400。
// decodeJSON decodes the request body into the target and writes 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(target); err != nil {
		writePublicError(w, http.StatusBadRequest)
		return false
	}
	return true
}

// mustRaw 是 testdata 专用的 panic-on-error 挂载辅助,仅在测试应用中安全使用。
// mustRaw is a testdata-only panic-on-error mount helper, safe only in test apps.
func mustRaw(err error) {
	if err != nil {
		panic("k6: " + err.Error())
	}
}

// trimSpaces 去掉首尾空格。
func trimSpaces(s string) string {
	return strings.TrimSpace(s)
}

package ghttp

import (
	"errors"
	"net/http"
)

// ===========================================================================
// 统一错误链:把 handler / codec / 校验返回的 error 收敛为规范的 HTTP 响应。
//   - 分类:实现了 StatusCoder 的 error 自带状态码;否则按框架哨兵映射;兜底 500。
//   - 脱敏:默认只回写与状态码绑定的通用文案,内部细节仅进服务端日志;经
//     WithExposeErrorDetails(true) 显式开启后才回传 err.Error()。
//   - 出口:writeError 是唯一出口,dispatchChained/dispatchRaw/panic 兜底都汇入它,
//     已提交(Written)则不改写,避免双写。
// Unified error chain: collapses errors from handlers/codecs/validation into a
// canonical HTTP response. Classification prefers a StatusCoder, then framework
// sentinels, else 500. Details are hidden by default (generic per-status text;
// internals go to the server log only) unless WithExposeErrorDetails(true).
// writeError is the single exit; committed responses are never rewritten.
// ===========================================================================

// statusError 是一个仅承载 HTTP 状态码的轻量 error,实现 StatusCoder,用于把
// 404/405 等 miss 情形以统一形态报告给 onError 钩子。
// statusError is a lightweight error carrying only an HTTP status; it implements
// StatusCoder to report miss cases (404/405, etc.) uniformly to the onError hook.
type statusErr int

func (e statusErr) Error() string   { return genericMessage(int(e)) }
func (e statusErr) HTTPStatus() int { return int(e) }

// statusError 返回一个承载 status 的 StatusCoder error。
// statusError returns a StatusCoder error carrying status.
func statusError(status int) error { return statusErr(status) }

// StatusCoder 让业务错误自带 HTTP 状态码。实现它即参与错误链的状态映射,
// 优先级高于框架哨兵映射(可精确控制 404/409/422 等)。
// StatusCoder lets a business error carry its own HTTP status. Implementing it
// participates in the error chain's status mapping with priority over sentinel
// mapping (for precise 404/409/422, etc.).
type StatusCoder interface {
	error
	HTTPStatus() int
}

// ErrorRenderer 把一个已分类的错误写成响应体。默认实现输出
// {"error":{"code","message"}} 的 JSON。用户可经 WithErrorRenderer 替换
// (如改为 RFC 9457 problem+json)。
// ErrorRenderer writes a classified error as a response body. The default emits
// {"error":{"code","message"}} JSON. Replace via WithErrorRenderer (e.g. for
// RFC 9457 problem+json).
type ErrorRenderer interface {
	RenderError(resp *Response, status int, code, message string)
}

// classifyError 返回 err 对应的 HTTP 状态码与稳定的机器可读 code 串。
// 优先 StatusCoder,其次框架哨兵,最后兜底 500 internal。
// classifyError returns the HTTP status and a stable machine-readable code for
// err: StatusCoder first, then framework sentinels, else a 500 fallback.
func classifyError(err error) (status int, code string) {
	var sc StatusCoder
	if errors.As(err, &sc) {
		s := sc.HTTPStatus()
		return s, codeForStatus(s)
	}
	switch {
	case errors.Is(err, ErrValidation):
		return http.StatusBadRequest, "validation_failed"
	case errors.Is(err, ErrMissingRequired):
		return http.StatusBadRequest, "missing_required"
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidRequestPath):
		return http.StatusBadRequest, "invalid_input"
	case errors.Is(err, ErrUnsupportedMediaType):
		return http.StatusUnsupportedMediaType, "unsupported_media_type"
	case errors.Is(err, ErrRequestEntityTooLarge):
		return http.StatusRequestEntityTooLarge, "request_entity_too_large"
	case errors.Is(err, ErrHandlerPanic):
		return http.StatusInternalServerError, "internal"
	}
	// http.MaxBytesError 由 LimitBody 的 MaxBytesReader 在解码期触发 → 413。
	// http.MaxBytesError from LimitBody's MaxBytesReader during decode → 413.
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return http.StatusRequestEntityTooLarge, "request_entity_too_large"
	}
	return http.StatusInternalServerError, "internal"
}

// codeForStatus 为一个状态码返回稳定的 code 串(供 StatusCoder 路径与 miss 复用)。
// codeForStatus returns a stable code string for a status (shared by the
// StatusCoder path and miss handling).
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_input"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type"
	case http.StatusRequestEntityTooLarge:
		return "request_entity_too_large"
	case http.StatusUnprocessableEntity:
		return "unprocessable_entity"
	case http.StatusServiceUnavailable:
		return "unavailable"
	}
	if status >= 500 {
		return "internal"
	}
	return "error"
}

// genericMessage 返回与状态码绑定的通用文案(脱敏默认)。不含任何内部细节。
// genericMessage returns the generic per-status text (the sanitized default),
// carrying no internal detail.
func genericMessage(status int) string {
	if msg := http.StatusText(status); msg != "" {
		return msg
	}
	return "error"
}

// jsonErrorRenderer 是默认 ErrorRenderer:输出 {"error":{"code","message"}} JSON。
// jsonErrorRenderer is the default ErrorRenderer emitting
// {"error":{"code","message"}} JSON.
type jsonErrorRenderer struct{}

func (jsonErrorRenderer) RenderError(resp *Response, status int, code, message string) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.WriteHeader(status)
	// 手工拼接单块 JSON,避免 json.NewEncoder 的 reflect/缓冲多次分配(404 洪水等
	// miss 冷路径也是真实生产场景)。code 来自受控集合无需转义;message 可能是
	// 用户开启细节后的任意 err.Error(),用 appendJSONString 做转义。
	// Hand-build a single JSON block to avoid json.NewEncoder's reflect/buffer
	// allocations (404 floods are a real production case). code comes from a
	// controlled set and needs no escaping; message may be an arbitrary
	// err.Error() when details are enabled, so escape it via appendJSONString.
	buf := make([]byte, 0, 48+len(code)+len(message))
	buf = append(buf, `{"error":{"code":`...)
	buf = appendJSONString(buf, code)
	buf = append(buf, `,"message":`...)
	buf = appendJSONString(buf, message)
	buf = append(buf, "}}"...)
	_, _ = resp.Write(buf)
}

// appendJSONString 把 s 作为带引号的 JSON 字符串追加到 dst,转义控制字符与 " \ 。
// 仅供错误体拼接使用,足以安全编码任意 err.Error()。
// appendJSONString appends s to dst as a quoted JSON string, escaping control
// characters and " \. Used only for error-body assembly; safe for arbitrary
// err.Error() content.
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 {
			dst = append(dst, s[start:i]...)
			switch c {
			case '"':
				dst = append(dst, '\\', '"')
			case '\\':
				dst = append(dst, '\\', '\\')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				const hex = "0123456789abcdef"
				dst = append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
			}
			start = i + 1
		}
	}
	dst = append(dst, s[start:]...)
	dst = append(dst, '"')
	return dst
}

// defaultErrorRenderer 是包级默认渲染器实例(无状态,可共享)。
// defaultErrorRenderer is the package-level default renderer (stateless, shared).
var defaultErrorRenderer ErrorRenderer = jsonErrorRenderer{}

// prebuiltMissBody 预构建高频 miss 状态码(404/405)在默认脱敏模式下的完整 JSON 错误体。
// miss 洪水(如扫描器、错误链接)是真实生产热点,预构建后请求期直接写切片,零分配、零拼接。
// 仅当使用默认渲染器时复用;自定义 ErrorRenderer 或业务错误仍走 jsonErrorRenderer 动态拼接。
// prebuiltMissBody holds the full JSON error bodies for high-frequency miss
// statuses (404/405) under the default sanitized renderer. Miss floods (scanners,
// dead links) are a real production hot spot; prebuilding lets the request path
// write the slice directly at zero alloc. Reused only with the default renderer.
var prebuiltMissBody = map[int][]byte{
	http.StatusNotFound:         buildMissBody(http.StatusNotFound),
	http.StatusMethodNotAllowed: buildMissBody(http.StatusMethodNotAllowed),
}

// buildMissBody 用与 jsonErrorRenderer 完全一致的格式构建 status 的脱敏错误体。
// buildMissBody builds the sanitized error body for status in the exact same
// format as jsonErrorRenderer.
func buildMissBody(status int) []byte {
	code, message := codeForStatus(status), genericMessage(status)
	buf := make([]byte, 0, 48+len(code)+len(message))
	buf = append(buf, `{"error":{"code":`...)
	buf = appendJSONString(buf, code)
	buf = append(buf, `,"message":`...)
	buf = appendJSONString(buf, message)
	buf = append(buf, "}}"...)
	return buf
}

// writeError 是错误链的唯一出口:分类 → 选状态码 → 按脱敏策略取文案 → 渲染。
// 若响应已提交则只经 onError 记录、绝不改写,避免双写。err 为 nil 时空操作。
// writeError is the error chain's single exit: classify → status → message per
// the sanitize policy → render. If already committed, it only reports via
// onError and never rewrites, avoiding a double write. A nil err is a no-op.
func (m *mux) writeError(resp *Response, r *http.Request, err error) {
	if err == nil {
		return
	}
	status, code := classifyError(err)

	if m.onError != nil {
		m.onError(r, status, err)
	}

	// 已提交:不能再改写头/体。分类与记录已完成,直接返回。
	// Already committed: cannot rewrite. Classification/logging done; return.
	if resp.Written() {
		return
	}

	message := genericMessage(status)
	if m.exposeErrorDetails {
		message = err.Error()
	}

	renderer := m.errorRenderer
	if renderer == nil {
		renderer = defaultErrorRenderer
	}
	renderer.RenderError(resp, status, code, message)
}

// ---------------------------------------------------------------------------
// 错误链相关 Option / Error-chain options
// ---------------------------------------------------------------------------

// WithExposeErrorDetails 控制错误响应体是否回传内部错误细节。默认 false(脱敏,
// 只回通用文案);置 true 时 message 带上 err.Error()(仅建议开发/内网调试用)。
// WithExposeErrorDetails controls whether error bodies leak internal detail.
// Default false (sanitized generic text); true puts err.Error() into message
// (dev/intranet debugging only).
func WithExposeErrorDetails(expose bool) Option {
	return func(s *Server) { s.exposeErrorDetails = expose }
}

// WithErrorRenderer 用自定义渲染器替换默认 JSON 错误体(如 problem+json)。
// nil 时回退到默认渲染器。
// WithErrorRenderer replaces the default JSON error body with a custom renderer
// (e.g. problem+json). A nil value falls back to the default renderer.
func WithErrorRenderer(rn ErrorRenderer) Option {
	return func(s *Server) { s.errorRenderer = rn }
}

// WithErrorHook 注册一个错误观测钩子:错误链分类后调用(无论是否已提交响应),
// 用于结构化日志/上报。它不写响应,只观测。
// WithErrorHook registers an error observation hook invoked after
// classification (whether or not the response is committed), for structured
// logging/reporting. It does not write the response, only observes.
func WithErrorHook(hook func(r *http.Request, status int, err error)) Option {
	return func(s *Server) { s.onError = hook }
}

package client

import (
	"errors"
	"fmt"
)

// 本文件定义 client 的错误模型：传输错误与 HTTP 状态错误统一收敛为 *Error，
// 且错误里带得走整个 *Response（响应体仍可读）。
// This file defines the client error model: transport failures and HTTP status
// failures both collapse into *Error, which carries the whole *Response so the body
// remains readable.

// 哨兵错误。用 errors.Is 判断类别，用 errors.As 取 *Error 拿细节。
// Sentinel errors. Use errors.Is for the category, errors.As for *Error details.
var (
	// ErrUnexpectedStatus 标记"收到了响应，但状态码不在可接受集合内"。它是 *Error 的类别标记，
	// 使调用方无需类型断言即可分流：
	//	errors.Is(err, client.ErrUnexpectedStatus)
	// ErrUnexpectedStatus marks "a response arrived whose status is not in the accepted
	// set". It is *Error's category marker, so callers can branch without a type
	// assertion.
	ErrUnexpectedStatus = errors.New("ghttp/client: unexpected HTTP status")

	// ErrNoBaseURL 表示既没给 WithBaseURL，也没给绝对 URL。
	// ErrNoBaseURL means neither WithBaseURL nor an absolute URL was provided.
	ErrNoBaseURL = errors.New("ghttp/client: base URL is empty")

	// ErrBodyTooLarge 表示响应体超过 WithResponseBodyLimit 设定的上限。它是刻意的安全默认：
	// 服务端被劫持或配置错误时返回的巨型错误页不应该把调用方内存打爆。
	// ErrBodyTooLarge means the response body exceeded WithResponseBodyLimit. It is a
	// deliberate safe default: a huge error page from a compromised or misconfigured
	// server must not blow up the caller's memory.
	ErrBodyTooLarge = errors.New("ghttp/client: response body exceeds limit")

	// ErrBodyNotReplayable 表示"请求体无法重放，因此拒绝重试"。它发生在发出第一次请求
	// 【之前】，而不是在产生副作用之后——这是对调用方最友好的失败时机。
	// ErrBodyNotReplayable means "the request body cannot be replayed, so retrying is
	// refused". It happens BEFORE the first attempt, not after a real side effect —
	// the most caller-friendly failure point.
	ErrBodyNotReplayable = errors.New("ghttp/client: request body cannot be replayed for retry")

	// ErrStreamConsumed 表示响应以流式模式打开后，又尝试把 body 读进内存（Bytes/String/Decode）。
	// 流式与内存是互斥的两种模式，混用必须报错而不是静默读一个已被消费的流。
	// ErrStreamConsumed means the response was opened in stream mode and the body was
	// then read into memory (Bytes/String/Decode). The two modes are mutually
	// exclusive; mixing them must fail loudly instead of silently reading a consumed
	// stream.
	ErrStreamConsumed = errors.New("ghttp/client: response is in stream mode")

	// ErrNoCodec 表示响应 Content-Type 没有对应的解码器，或请求体类型无从推断编码。
	// ErrNoCodec means no decoder is registered for the response Content-Type, or the
	// request body's encoding cannot be inferred.
	ErrNoCodec = errors.New("ghttp/client: no codec for content type")

	// ErrDecode 标记"响应体解码失败"（格式错误、截断、尾随内容）。底层错误经 %w 保留，
	// 可 errors.As 出 *json.SyntaxError 等具体类型。
	// ErrDecode marks a response-body decode failure (bad syntax, truncation, trailing
	// content). The cause is preserved via %w, so errors.As can extract concrete types
	// such as *json.SyntaxError.
	ErrDecode = errors.New("ghttp/client: response decode failed")
)

// Error 是一次失败请求的统一错误。传输层失败时 StatusCode 为 0；HTTP 状态失败时
// StatusCode 非 0 且 Response 非 nil。
//
// 它实现 httperr.StatusCoder（即 ghttp.StatusCoder），因此 server 侧按状态码分支的
// 断言代码在 client 侧原样可用。
// Error is the unified error for a failed request. On a transport failure StatusCode
// is 0; on an HTTP status failure StatusCode is non-zero and Response is non-nil.
//
// It implements httperr.StatusCoder (i.e. ghttp.StatusCoder), so server-side branching
// code written against the status contract works unchanged on the client.
type Error struct {
	// Op 是发起本次请求的方法名（"Get"/"Post"/…），便于日志归因。
	// Op is the method that issued the request ("Get"/"Post"/…), for log attribution.
	Op string
	// Method 与 URL 是本次请求实际使用的方法与完整 URL。
	// Method and URL are the method and full URL actually used.
	Method string
	URL    string
	// StatusCode 为 0 表示根本没收到响应（传输层失败）；非 0 表示收到了非接受的响应。
	// StatusCode 0 means no response arrived at all (transport failure); non-zero means
	// a non-accepted response was received.
	StatusCode int
	Status     string
	// Attempts 是最终失败前的总尝试次数（含第一次）。重试关闭时恒为 1。
	// Attempts is the total number of attempts (including the first) before the final
	// failure. It is always 1 when retries are off.
	Attempts int
	// Err 是底层错误：传输失败时是 net/http 的错误，响应解码失败时是解码错误。
	// 它经 Unwrap 暴露，因此 errors.Is/As 能穿透到 *url.Error、*json.SyntaxError 等。
	// Err is the underlying error: the net/http error on transport failure, the decode
	// error on a body failure. Unwrap exposes it, so errors.Is/As reach *url.Error,
	// *json.SyntaxError and friends.
	Err error
	// Response 在收到响应时非 nil（即使状态码不可接受），body 按响应体策略保留。
	// Response is non-nil whenever a response arrived (even with an unacceptable
	// status); its body is retained per the response-body policy.
	Response *Response
}

// Error 输出一行可读描述，包含方法、URL、状态码与尝试次数。
// Error renders a one-line description with method, URL, status and attempt count.
func (e *Error) Error() string {
	var b []byte
	b = append(b, "ghttp/client: "...)
	if e.Op != "" {
		b = append(b, e.Op...)
		b = append(b, ' ')
	}
	if e.Method != "" {
		b = append(b, e.Method...)
		b = append(b, ' ')
	}
	b = append(b, e.URL...)
	if e.StatusCode != 0 {
		b = append(b, ": "...)
		b = append(b, e.Status...)
	}
	if e.Attempts > 1 {
		b = fmt.Appendf(b, " (attempts=%d)", e.Attempts)
	}
	if e.Err != nil {
		b = append(b, ": "...)
		b = append(b, e.Err.Error()...)
	}
	return string(b)
}

// Unwrap 暴露底层错误，使 errors.Is/errors.As 能穿透到传输错误或解码错误的根因。
// Unwrap exposes the cause so errors.Is/errors.As reach the transport or decode root.
func (e *Error) Unwrap() error { return e.Err }

// HTTPStatus 实现 StatusCoder：返回实际收到的状态码；传输失败（未收到响应）返回 0。
// HTTPStatus implements StatusCoder: the status actually received, or 0 when no
// response arrived.
func (e *Error) HTTPStatus() int { return e.StatusCode }

// Is 让 errors.Is 支持两类判断：状态错误类别标记（ErrUnexpectedStatus）与底层错误的
// 常规匹配。
// Is supports two kinds of errors.Is query: the status-error category marker
// (ErrUnexpectedStatus) and ordinary matching against the cause.
func (e *Error) Is(target error) bool {
	if target == ErrUnexpectedStatus {
		return e.StatusCode != 0
	}
	if e.Err != nil {
		return errors.Is(e.Err, target)
	}
	return false
}

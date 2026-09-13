package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"

	"github.com/sofiworker/gk/ghttp/internal/logsafe"
	"github.com/sofiworker/gk/gretry"
)

// 本文件实现 HTTP 重试：判定条件与"请求体能否重放"留在本包（HTTP 专属），退避算法复用
// 基础契约层的 gretry（依赖策略要求公共概念只实现一次）。
// This file implements HTTP retries: the decision conditions and "can the body be
// replayed" stay here (HTTP-specific), while the backoff algorithm is reused from the
// base-contract layer's gretry (the dependency policy requires one implementation of a
// shared concept).

// RetryPolicy 描述一次重试策略。零值表示不重试。
// RetryPolicy describes a retry strategy. Its zero value means no retries.
type RetryPolicy struct {
	// MaxRetries 是首次尝试之外的最大重试次数。
	// MaxRetries is the maximum number of retries beyond the first attempt.
	MaxRetries int

	// ShouldRetry 完全接管重试判定；为 nil 时使用默认条件（见 RetryPolicy.shouldRetry）。
	// ShouldRetry takes over the decision entirely; nil means the default conditions
	// (see RetryPolicy.shouldRetry).
	ShouldRetry func(resp *Response, err error) bool

	// RetryDelay / MaxDelay / Strategy / Jitter / JitterFactor 透传给 gretry.NextDelay。
	// RetryDelay / MaxDelay / Strategy / Jitter / JitterFactor are passed through to
	// gretry.NextDelay.
	RetryDelay   time.Duration
	MaxDelay     time.Duration
	Strategy     gretry.RetryStrategy
	Jitter       gretry.JitterType
	JitterFactor float64

	// RespectRetryAfter 让服务端的 Retry-After（秒数或 HTTP-date）优先于本地退避。
	// RespectRetryAfter lets the server's Retry-After (seconds or HTTP-date) override the
	// local backoff.
	RespectRetryAfter bool

	// RetryNonIdempotent 放开"只重试幂等方法"的限制。放开后 POST/PATCH 也会重试，
	// 可能产生重复副作用，请确认对端幂等后再开。
	// RetryNonIdempotent lifts the "idempotent methods only" restriction. POST/PATCH then
	// retry too, which can duplicate side effects — only enable it once the peer is known
	// to be idempotent.
	RetryNonIdempotent bool

	// OnRetry 在每次重试等待之前调用。
	// OnRetry runs before each retry's wait.
	OnRetry func(attempt int, delay time.Duration, resp *Response, err error)
}

// WithRetry 设置 Client 级重试策略。
// WithRetry sets the client-level retry policy.
func WithRetry(policy RetryPolicy) Option {
	return func(c *Client) { c.retry = policy }
}

// SetRetry 设置本请求的重试策略，覆盖 Client 级设置。
// SetRetry sets this request's retry policy, overriding the client-level one.
func (r *Request) SetRetry(policy RetryPolicy) *Request {
	r.retry = policy
	r.retrySet = true
	return r
}

// retryPolicyOf 取本次请求实际生效的重试策略：请求级优先于 Client 级。
// retryPolicyOf returns the effective retry policy: request-level beats client-level.
func (c *Client) retryPolicyOf(r *Request) RetryPolicy {
	if r.retrySet {
		return r.retry
	}
	return c.retry
}

// gretryOptions 把本策略翻译成 gretry 的选项，仅取退避相关字段（判定逻辑留在本包）。
// gretryOptions translates this policy into gretry options, taking only the backoff
// fields (the decision logic stays here).
func (p RetryPolicy) gretryOptions() gretry.ErrorHandlingOptions {
	opts := gretry.DefaultErrorHandlingOptions
	opts.MaxRetries = p.MaxRetries
	if p.RetryDelay > 0 {
		opts.RetryDelay = p.RetryDelay
	}
	if p.MaxDelay > 0 {
		opts.MaxRetryDelay = p.MaxDelay
	}
	if p.Strategy != "" {
		opts.RetryStrategy = p.Strategy
	}
	if p.Jitter != "" {
		opts.JitterType = p.Jitter
	}
	if p.JitterFactor > 0 {
		opts.JitterFactor = p.JitterFactor
	}
	return opts
}

// shouldRetry 是默认重试判定：幂等方法 + 传输错误或可重试状态码。
//
// 为何默认按方法白名单：POST/PATCH 不是幂等的，无差别重试会在超时后产生重复副作用
// （重复下单、重复扣款）。生态里 hashicorp/go-retryablehttp 默认完全不看方法，这是已知
// 的事故来源；resty 则默认只重试幂等方法。本包采用后者并保留显式放开开关。
// shouldRetry is the default decision: idempotent methods plus transport errors or
// retryable statuses.
//
// Why a method allowlist by default: POST/PATCH are not idempotent, and retrying them
// indiscriminately duplicates side effects after a timeout (double orders, double
// charges). In the ecosystem hashicorp/go-retryablehttp ignores the method entirely —
// a known incident source — while resty retries only idempotent methods by default.
// This package follows the latter and keeps an explicit opt-out.
func (p RetryPolicy) shouldRetry(resp *Response, err error, method string) bool {
	if p.ShouldRetry != nil {
		return p.ShouldRetry(resp, err)
	}
	if !p.RetryNonIdempotent && !isIdempotentMethod(method) {
		return false
	}
	if err != nil {
		return true
	}
	if resp == nil {
		return false
	}
	switch resp.StatusCode() {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// isIdempotentMethod 报告方法是否幂等（重放不改变最终状态）。按 RFC 9110 的语义：
// GET/HEAD/OPTIONS/TRACE 安全且幂等，PUT/DELETE 幂等。
// isIdempotentMethod reports whether replaying the method leaves the final state
// unchanged. Per RFC 9110: GET/HEAD/OPTIONS/TRACE are safe and idempotent, PUT/DELETE
// are idempotent.
func isIdempotentMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace,
		http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// ensureReplayable 在发出第一次请求【之前】判定请求体能否重放；不能则直接拒绝。
//
// 这是对调用方最友好的失败时机：错误发生在可归因的配置期，而不是在一次真实副作用之后。
// imroc/req v3 采取同样立场；resty 是发完第一次才发现不可 seek；go-retryablehttp 则
// 无上限地 io.ReadAll 缓冲整个 body。
// ensureReplayable decides BEFORE the first attempt whether the body can be replayed,
// refusing outright when it cannot.
//
// That is the most caller-friendly failure point: it happens at attributable
// configuration time rather than after a real side effect. imroc/req v3 takes the same
// stance; resty only discovers non-seekability after the first attempt; and
// go-retryablehttp buffers the whole body with an unbounded io.ReadAll.
func ensureReplayable(r *Request) error {
	if r.isMultipart {
		if !r.multipartReplayable() {
			return fmt.Errorf("%w: a multipart reader is not an io.ReadSeeker; use SetFile (path) or a seekable reader, or disable retries", ErrBodyNotReplayable)
		}
		return nil
	}
	if r.bodyReader == nil {
		// 内存载体（SetJSON/SetForm/SetBody）每次都由 encodeBody 重新生成，天然可重放。
		// In-memory carriers (SetJSON/SetForm/SetBody) are regenerated by encodeBody on
		// every attempt and are therefore replayable by construction.
		return nil
	}
	if _, ok := r.bodyReader.(io.Seeker); ok {
		return nil
	}
	return fmt.Errorf("%w: use an io.ReadSeeker or an in-memory body (SetJSON/SetForm/SetBody), or disable retries", ErrBodyNotReplayable)
}

// resetBody 在两次尝试之间把可 seek 的请求体归零。
// resetBody rewinds a seekable body between attempts.
func resetBody(r *Request) error {
	if r.isMultipart {
		return r.rewindMultipartReaders()
	}
	if r.bodyReader == nil {
		return nil
	}
	s, ok := r.bodyReader.(io.Seeker)
	if !ok {
		return ErrBodyNotReplayable
	}
	if _, err := s.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("ghttp/client: rewind request body: %w", err)
	}
	return nil
}

// retryAfter 解析 Retry-After 头：整数秒或 HTTP-date 两种形式（RFC 9110）。
// 第二个返回值为 false 表示头缺失或无法解析，调用方应回退到本地退避。
// retryAfter parses the Retry-After header in either form (integer seconds or an
// HTTP-date, per RFC 9110). A false second result means missing/unparseable, and the
// caller should fall back to local backoff.
func retryAfter(resp *Response) (time.Duration, bool) {
	if resp == nil || resp.Header() == nil {
		return 0, false
	}
	value := strings.TrimSpace(resp.Header().Get("Retry-After"))
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// sendWithRetry 是重试循环：每次尝试都重新构建 http.Request（内存载体重新编码、可 seek
// 的 reader 归零），因此重放的是同一份语义内容。
// sendWithRetry is the retry loop: every attempt rebuilds the http.Request (in-memory
// carriers re-encode, seekable readers rewind), so what gets replayed is the same
// semantic content.
func (c *Client) sendWithRetry(ctx context.Context, r *Request) (*Response, error) {
	policy := c.retryPolicyOf(r)
	if policy.MaxRetries <= 0 {
		return c.attempt(ctx, r)
	}
	if err := ensureReplayable(r); err != nil {
		return nil, err
	}
	opts := policy.gretryOptions()

	var lastResp *Response
	var lastErr error
	attempts := 0

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := gretry.NextDelay(attempt-1, opts)
			if policy.RespectRetryAfter {
				if d, ok := retryAfter(lastResp); ok {
					delay = d
				}
			}
			if policy.OnRetry != nil {
				policy.OnRetry(attempt, delay, lastResp, lastErr)
			}
			if werr := gretry.Wait(ctx, delay); werr != nil {
				return lastResp, werr
			}
			if rerr := resetBody(r); rerr != nil {
				return lastResp, rerr
			}
		}
		attempts++
		resp, err := c.attempt(ctx, r)
		if resp != nil {
			resp.attempts = attempts
		}
		lastResp, lastErr = resp, err
		if !policy.shouldRetry(resp, err, r.method) {
			return resp, err
		}
	}
	return lastResp, lastErr
}

// attempt 是一次尝试：构建请求、发送、读入响应。
// attempt is one try: build, send, read in.
func (c *Client) attempt(ctx context.Context, r *Request) (*Response, error) {
	// 仅在显式开启时建采集器并注入 context；关闭时零开销。
	// Build the collector and inject the context only when explicitly enabled; off
	// means zero overhead.
	var tc *traceCollector
	if c.trace {
		tc = newTraceCollector()
		ctx = httptrace.WithClientTrace(ctx, tc.clientTrace())
	}
	httpReq, err := c.buildRequest(ctx, r)
	if err != nil {
		return nil, err
	}
	if c.debug && c.logger != nil {
		c.logger.Debugf("ghttp/client: --> %s %s", httpReq.Method, logsafe.Token(httpReq.URL.String()))
	}
	start := time.Now()
	raw, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start)
	if err != nil {
		if c.debug && c.logger != nil {
			c.logger.Debugf("ghttp/client: <-- error after %s: %v", elapsed, err)
		}
		if raw != nil {
			// CheckRedirect 失败时标准库已关闭 body，这里再关一次是幂等的安全网。
			// When CheckRedirect fails the standard library already closed the body;
			// closing again is an idempotent safety net.
			_ = raw.Body.Close()
		}
		return nil, err
	}
	if c.debug && c.logger != nil {
		c.logger.Debugf("ghttp/client: <-- %s in %s", raw.Status, elapsed)
	}
	resp, err := c.readResponse(r, raw, elapsed)
	if tc != nil && resp != nil {
		resp.trace = tc.info(elapsed)
	}
	return resp, err
}

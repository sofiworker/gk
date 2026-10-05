package ghttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
)

// ETagDefaultMaxBytes 是 ETag 中间件默认的最大缓冲字节数（1 MiB）。
// ETagDefaultMaxBytes is the default maximum number of bytes ETag buffers (1 MiB).
const ETagDefaultMaxBytes = 1 << 20

// etagConfig 是 ETag 的配置。
// etagConfig is the ETag configuration.
type etagConfig struct {
	maxBytes int
}

// ETagOption 配置 ETag。
// ETagOption configures ETag.
type ETagOption func(*etagConfig)

// WithETagMaxBytes 设置缓冲上限；响应体超过后放弃计算并直通。<= 0 回退为默认值（1 MiB）。
// WithETagMaxBytes sets the buffering cap; once exceeded the response is passed through
// uncomputed. <= 0 falls back to the default (1 MiB).
func WithETagMaxBytes(n int) ETagOption {
	return func(c *etagConfig) { c.maxBytes = n }
}

// ETag 返回为 GET/HEAD 的 200 响应自动生成弱 ETag 并处理 If-None-Match 的中间件。
//
// 行为：缓冲响应体（上限 WithETagMaxBytes，超过或遇到 Flush 则放弃并直通）；状态 200 且 handler
// 未设置 ETag 时计算 W/"<长度十六进制>-<sha256 前 8 字节十六进制>"。选用 sha256 截断而非 fnv：
// 开销相对网络 IO 可忽略，并避免攻击者构造哈希碰撞让不同内容共享同一验证器。
// If-None-Match 命中（支持 "*"、逗号列表、弱比较）时返回 304：不写 body，保留 ETag、Cache-Control、
// Vary 等头，删除 Content-Length 与 Content-Type。handler 自行设置了 ETag 时不再缓冲，直接据此判断。
// 非 GET/HEAD 或非 200 的响应不受影响。
//
// 顺序：应放在 Gzip 的内侧（后注册），这样 ETag 基于未压缩内容计算，Gzip 再把它降级为弱 ETag。
//
// ETag returns middleware that adds a weak ETag to 200 responses of GET/HEAD and handles
// If-None-Match.
//
// It buffers the body (capped by WithETagMaxBytes; exceeding it or a Flush abandons the
// computation and passes through). For status 200 without a handler-set ETag it computes
// W/"<hex length>-<hex of first 8 bytes of sha256>". sha256 truncation is chosen over fnv: its
// cost is negligible next to network IO and it keeps attackers from crafting hash collisions
// that make different contents share a validator. A matching If-None-Match (supporting "*",
// comma lists and weak comparison) yields 304 with no body, keeping ETag, Cache-Control, Vary
// etc. and dropping Content-Length and Content-Type. When the handler sets its own ETag no
// buffering is done; the decision is made from that value. Other methods and non-200 statuses
// are untouched.
//
// Ordering: place it inside Gzip (registered later) so the ETag is computed over uncompressed
// content and Gzip then weakens it.
func ETag(opts ...ETagOption) Middleware {
	cfg := etagConfig{maxBytes: ETagDefaultMaxBytes}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.maxBytes <= 0 {
		cfg.maxBytes = ETagDefaultMaxBytes
	}
	limit := cfg.maxBytes

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			r := req.Raw
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				return next(ctx, req, resp)
			}
			orig := resp.Writer
			w := &etagWriter{
				orig:  orig,
				max:   limit,
				inm:   r.Header.Values("If-None-Match"),
				state: etagBuffering,
			}
			resp.Writer = w
			defer func() { resp.Writer = orig }()
			err := next(ctx, req, resp)
			if err != nil {
				// handler 已开始写入时按原样提交缓冲（Response 已标记为 written）；否则不写任何内容，交给外层错误链
				// If the handler already started writing, commit the buffer as is (Response is marked
				// written); otherwise write nothing and leave the error to the outer chain.
				if w.state == etagBuffering && w.hasStatus {
					w.passthrough()
				}
				return err
			}
			w.finish(resp)
			return nil
		}
	}
}

// etag 写入器状态。
// etag writer states.
const (
	etagBuffering   = iota // 缓冲中 / buffering
	etagPassthrough        // 直通 / pass through
	etagDiscard            // 已决定 304，丢弃后续写入 / 304 decided, discard writes
)

// etagWriter 是缓冲响应体以计算 ETag 的 ResponseWriter。
// etagWriter is a ResponseWriter that buffers the body to compute an ETag.
type etagWriter struct {
	orig http.ResponseWriter
	max  int
	inm  []string

	state     int
	status    int
	hasStatus bool
	sent      bool
	buf       []byte
}

var (
	_ http.ResponseWriter = (*etagWriter)(nil)
	_ http.Flusher        = (*etagWriter)(nil)
)

// Header 返回底层响应头。
// Header returns the underlying headers.
func (w *etagWriter) Header() http.Header { return w.orig.Header() }

// Unwrap 返回底层写入器。
// Unwrap returns the underlying writer.
func (w *etagWriter) Unwrap() http.ResponseWriter { return w.orig }

// WriteHeader 记录状态码；非 200 立即直通，200 且已有 ETag 时立即决策。
// WriteHeader records the status; non-200 passes through at once, 200 with an existing ETag
// decides at once.
func (w *etagWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.orig.WriteHeader(code)
		return
	}
	if w.hasStatus {
		return
	}
	w.status, w.hasStatus = code, true
	w.decide()
}

// decide 在状态确定后做能立即做出的决定。
// decide makes whatever decision is possible once the status is known.
func (w *etagWriter) decide() {
	if w.state != etagBuffering {
		return
	}
	if w.status != http.StatusOK {
		w.passthrough()
		return
	}
	h := w.orig.Header()
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n > int64(w.max) {
			w.passthrough()
			return
		}
	}
	if et := h.Get("Etag"); et != "" {
		if etagMatch(w.inm, et) {
			w.notModified()
		} else {
			w.passthrough()
		}
	}
}

// passthrough 切换为直通：写出状态与已缓冲数据。
// passthrough switches to pass-through: it sends the status and any buffered data.
func (w *etagWriter) passthrough() {
	w.state = etagPassthrough
	if !w.hasStatus {
		w.status, w.hasStatus = http.StatusOK, true
	}
	w.sent = true
	w.orig.WriteHeader(w.status)
	if len(w.buf) > 0 {
		b := w.buf
		w.buf = nil
		_, _ = w.orig.Write(b)
	}
}

// notModified 写出 304 并进入丢弃状态。
// notModified sends 304 and enters the discard state.
func (w *etagWriter) notModified() {
	h := w.orig.Header()
	h.Del("Content-Length")
	h.Del("Content-Type")
	w.state = etagDiscard
	w.buf = nil
	w.sent = true
	w.orig.WriteHeader(http.StatusNotModified)
}

// Write 写入响应体：缓冲，超过上限则直通。
// Write writes the body: buffers, and passes through once the cap is exceeded.
func (w *etagWriter) Write(p []byte) (int, error) {
	if !w.hasStatus {
		w.WriteHeader(http.StatusOK)
	}
	switch w.state {
	case etagPassthrough:
		return w.orig.Write(p)
	case etagDiscard:
		return len(p), nil
	}
	if len(w.buf)+len(p) > w.max {
		w.passthrough()
		return w.orig.Write(p)
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// FlushError 表示流式输出：放弃计算并直通后再刷新。
// FlushError signals streaming: it abandons computation, passes through and then flushes.
func (w *etagWriter) FlushError() error {
	if w.state == etagBuffering {
		w.passthrough()
	}
	return http.NewResponseController(w.orig).Flush()
}

// Flush 实现 http.Flusher。
// Flush implements http.Flusher.
func (w *etagWriter) Flush() { _ = w.FlushError() }

// finish 在 handler 返回后收尾：计算 ETag，处理 If-None-Match，提交缓冲。
// finish wraps up after the handler: computes the ETag, handles If-None-Match, commits the buffer.
func (w *etagWriter) finish(resp *Response) {
	if w.state == etagDiscard {
		// 让访问日志看到真实状态 / let access logs see the real status
		resp.statusCode, resp.size = http.StatusNotModified, 0
		return
	}
	if w.state != etagBuffering || !w.hasStatus {
		return
	}
	h := w.orig.Header()
	et := h.Get("Etag")
	if et == "" {
		et = etagCompute(w.buf)
		h.Set("Etag", et)
	}
	if etagMatch(w.inm, et) {
		w.notModified()
		resp.statusCode, resp.size = http.StatusNotModified, 0
		return
	}
	if h.Get("Content-Length") == "" && h.Get("Content-Encoding") == "" {
		h.Set("Content-Length", strconv.Itoa(len(w.buf)))
	}
	w.passthrough()
}

// etagCompute 计算弱 ETag。
// etagCompute computes a weak ETag.
func etagCompute(b []byte) string {
	sum := sha256.Sum256(b)
	return `W/"` + strconv.FormatInt(int64(len(b)), 16) + "-" + hex.EncodeToString(sum[:8]) + `"`
}

// etagMatch 按弱比较判断 If-None-Match 是否命中 etag。
// etagMatch reports whether If-None-Match matches etag using weak comparison.
func etagMatch(inm []string, etag string) bool {
	want := strings.TrimPrefix(strings.TrimSpace(etag), "W/")
	for _, v := range inm {
		for _, tok := range etagSplitList(v) {
			if tok == "*" {
				return true
			}
			if strings.TrimPrefix(tok, "W/") == want {
				return true
			}
		}
	}
	return false
}

// etagSplitList 按逗号切分实体标签列表（引号内的逗号不切分）。
// etagSplitList splits an entity-tag list on commas (commas inside quotes are kept).
func etagSplitList(s string) []string {
	var out []string
	start, inQuote := 0, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				if t := strings.TrimSpace(s[start:i]); t != "" {
					out = append(out, t)
				}
				start = i + 1
			}
		}
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		out = append(out, t)
	}
	return out
}

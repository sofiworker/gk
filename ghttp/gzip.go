package ghttp

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// ===========================================================================
// Gzip 响应压缩中间件 / Gzip response-compression middleware
//
// 标准库 compress/gzip 实现,零第三方依赖。仅当请求 Accept-Encoding 含 gzip 且响应
// Content-Type 属于可压缩白名单(text/* 与 JSON/XML/JS/SVG 等结构化类型)时才压缩;
// 已带 Content-Encoding 的响应与无 body 状态码(204/304/1xx)直接透传。gzip.Writer
// 按压缩级别池化,避免每请求的大分配。压缩发生在中间件层,未挂载时零影响。
//
// Implemented on compress/gzip, zero third-party deps. Compression happens only
// when the request's Accept-Encoding includes gzip AND the response Content-Type
// is in the compressible whitelist (text/* and structured types like
// JSON/XML/JS/SVG); responses that already carry a Content-Encoding, and
// body-less statuses (204/304/1xx), pass through. gzip.Writer is pooled per
// compression level to avoid large per-request allocations. Compression lives in
// the middleware layer — zero impact when not mounted.
// ===========================================================================

// GzipOption 配置 Gzip 中间件。
// GzipOption configures the Gzip middleware.
type GzipOption func(*gzipConfig)

type gzipConfig struct {
	level        int
	contentTypes map[string]bool // 小写媒体类型集合(去参数);nil 用默认白名单 / lowercased media-type set (params stripped); nil uses the default whitelist
}

// WithGzipLevel 设置压缩级别(1-9,-1 默认,-2 HuffmanOnly);非法值 panic(注册期配置错误
// 尽早暴露)。
// WithGzipLevel sets the compression level (1-9, -1 default, -2 HuffmanOnly); an
// invalid value panics (registration-time config error surfaced early).
func WithGzipLevel(level int) GzipOption {
	return func(c *gzipConfig) { c.level = level }
}

// WithGzipContentTypes 覆盖可压缩 Content-Type 白名单(媒体类型,参数自动剥离)。默认:
// text/* 前缀 + application/json、javascript、xml、xhtml+xml、rss+xml、atom+xml、
// graphql、x-ndjson 与 image/svg+xml。
// WithGzipContentTypes overrides the compressible Content-Type whitelist (media
// types; parameters are stripped automatically). Default: the text/* prefix plus
// application/json, javascript, xml, xhtml+xml, rss+xml, atom+xml, graphql,
// x-ndjson, and image/svg+xml.
func WithGzipContentTypes(types ...string) GzipOption {
	return func(c *gzipConfig) {
		set := make(map[string]bool, len(types))
		for _, t := range types {
			set[mediaType(t)] = true
		}
		c.contentTypes = set
	}
}

func defaultGzipContentTypes() map[string]bool {
	return map[string]bool{
		"application/json":       true,
		"application/javascript": true,
		"application/xml":        true,
		"application/xhtml+xml":  true,
		"application/rss+xml":    true,
		"application/atom+xml":   true,
		"application/graphql":    true,
		"application/x-ndjson":   true,
		"image/svg+xml":          true,
	}
}

// gzipWriterPools 按压缩级别(索引 level+2,-2..9 映射到 0..11)池化 gzip.Writer,避免
// 高 RPS 下每请求分配压缩器内部状态(数百 KB)。
// gzipWriterPools pools gzip.Writer per compression level (level+2 maps -2..9 to
// 0..11) to avoid allocating the compressor's internal state (hundreds of KB) per
// request at high RPS.
var gzipWriterPools [12]sync.Pool

func getGzipWriter(w io.Writer, level int) *gzip.Writer {
	p := &gzipWriterPools[level+2]
	if v := p.Get(); v != nil {
		gw := v.(*gzip.Writer)
		gw.Reset(w)
		return gw
	}
	gw, _ := gzip.NewWriterLevel(w, level)
	return gw
}

func putGzipWriter(gw *gzip.Writer, level int) {
	gw.Reset(io.Discard) // 解除对上一个响应 writer 的引用,防止池化对象 pin 住连接。
	gzipWriterPools[level+2].Put(gw)
}

// Gzip 返回响应压缩中间件。压缩决策发生在 WriteHeader:Content-Type 白名单外、已设
// Content-Encoding、无 body 状态码均透传;压缩时自动设置 Content-Encoding: gzip 并
// 删除 Content-Length。支持 Flush 透传(SSE 等流式场景);压缩开始后不支持 Hijack。
// Gzip returns the response-compression middleware. The decision happens at
// WriteHeader: content types outside the whitelist, an already-set
// Content-Encoding, and body-less statuses pass through; when compressing it sets
// Content-Encoding: gzip and deletes Content-Length. Flush passes through (for
// streaming such as SSE); Hijack is unsupported once compression has started.
func Gzip(opts ...GzipOption) Middleware {
	cfg := gzipConfig{level: gzip.DefaultCompression, contentTypes: defaultGzipContentTypes()}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.level < gzip.HuffmanOnly || cfg.level > gzip.BestCompression {
		panic("ghttp: Gzip: invalid level")
	}
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			// 挂载本中间件后,同一 URL 的响应体随 Accept-Encoding 而变(压缩/未压缩两种
			// 形态),必须声明 Vary,且【无论本次请求是否接受 gzip】:否则共享缓存会把
			// 未压缩响应当作唯一形态缓存,或把 gzip 响应喂给不支持的客户端。
			// Once this middleware is mounted, one URL's body varies with
			// Accept-Encoding (compressed vs. not), so Vary must be declared — and
			// REGARDLESS of whether this request accepts gzip: otherwise a shared
			// cache stores the uncompressed response as the only variant, or feeds
			// the gzip one to clients that cannot decode it.
			ensureVary(resp.Header(), "Accept-Encoding")
			if !acceptsGzip(req.Header.Get("Accept-Encoding")) {
				return next(ctx, req, resp)
			}
			orig := resp.ResponseWriter
			w := &gzipResponseWriter{resp: resp, cfg: &cfg, orig: orig}
			resp.ResponseWriter = w
			// 恢复必须 defer:next 返回 error 时,统一错误链在中间件链【外】写错误体,
			// 届时写回原始 writer(未压缩),避免 Content-Encoding 与实际编码不一致。
			// The restore must be deferred: when next returns an error, the unified
			// error chain writes the error body OUTSIDE the middleware chain, to
			// the original writer (uncompressed), avoiding a Content-Encoding
			// mismatch with the actual encoding.
			defer func() {
				if w.gz != nil {
					_ = w.gz.Close()
					putGzipWriter(w.gz, cfg.level)
				}
				resp.ResponseWriter = orig
			}()
			return next(ctx, req, resp)
		}
	}
}

// gzipResponseWriter 包装底层 writer,在 WriteHeader 时决策是否压缩,并把 gzip 字节
// 直接写入底层,同时保持 Response 的 status/bytesOut 追踪(经 Response 自身)。
// gzipResponseWriter wraps the underlying writer, decides at WriteHeader whether
// to compress, writes gzip bytes straight through to the underlying writer, and
// keeps Response's status/bytesOut tracking intact (via Response itself).
type gzipResponseWriter struct {
	resp     *Response
	cfg      *gzipConfig
	orig     http.ResponseWriter
	gz       *gzip.Writer
	decided  bool
	compress bool
}

func (w *gzipResponseWriter) Header() http.Header { return w.orig.Header() }

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.decided {
		return
	}
	w.decided = true
	w.compress = w.shouldCompress(status)
	if w.compress {
		w.resp.Header().Set("Content-Encoding", "gzip")
		w.resp.Header().Del("Content-Length")
		w.gz = getGzipWriter(w.orig, w.cfg.level)
	}
	w.orig.WriteHeader(status)
}

func (w *gzipResponseWriter) shouldCompress(status int) bool {
	if !bodyAllowed(status) {
		return false
	}
	if w.resp.Header().Get("Content-Encoding") != "" {
		return false
	}
	ct := mediaType(w.resp.Header().Get("Content-Type"))
	if ct == "" {
		return false
	}
	if w.cfg.contentTypes[ct] {
		return true
	}
	// 默认白名单含 text/* 前缀;自定义白名单精确匹配。若无 text 前缀规则,text/* 也压。
	// The default whitelist includes the text/* prefix; a custom whitelist is exact.
	// text/* still compresses when no explicit text rule is configured.
	return strings.HasPrefix(ct, "text/")
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	if w.compress {
		return w.gz.Write(b)
	}
	return w.orig.Write(b)
}

// WriteString 避免经 []byte 转换的额外分配;压缩路径 gzip.Writer 无 WriteString,
// 转换一次可接受(池化已摊薄成本)。
// WriteString avoids the extra allocation of a []byte conversion; gzip.Writer has
// no WriteString, so one conversion on the compression path is acceptable (the
// pool already amortizes cost).
func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	if w.compress {
		return w.gz.Write([]byte(s))
	}
	if sw, ok := w.orig.(io.StringWriter); ok {
		return sw.WriteString(s)
	}
	return w.orig.Write([]byte(s))
}

// Flush 透传:压缩路径先冲刷 gzip 内部缓冲,再冲刷底层连接(SSE 等流式场景)。
// Flush passes through: on the compression path it flushes the gzip internal
// buffer, then the underlying connection (for streaming such as SSE).
func (w *gzipResponseWriter) Flush() {
	if w.compress && w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.orig.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 仅在压缩未开始时透传;已压缩则报错(gzip 流无法归还连接所有权)。
// Hijack passes through only before compression has started; after that it errors
// (a gzip stream cannot hand the connection back).
func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.compress {
		return nil, nil, errors.New("ghttp: gzip middleware cannot hijack after compression started")
	}
	if h, ok := w.orig.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("ghttp: underlying ResponseWriter does not support hijacking")
}

// Unwrap 暴露底层 writer,供 http.ResponseController 等标准库机制取回原始能力。
// Unwrap exposes the underlying writer for stdlib mechanisms such as
// http.ResponseController.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.orig }

// ensureVary 幂等地把 value 追加进 Vary 头:已声明(含大小写变体、逗号合并列表)则不
// 重复追加。Vary 语义是集合,重复项虽合法但会让下游缓存键解析做无谓工作,也易被误读。
// ensureVary appends value to the Vary header idempotently: if already declared
// (case-insensitively, including within comma-joined lists) nothing is added.
// Vary is a set; duplicates are legal but make downstream cache-key parsing do
// pointless work and are easy to misread.
func ensureVary(h http.Header, value string) {
	for _, existing := range h.Values("Vary") {
		for _, item := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	h.Add("Vary", value)
}

// acceptsGzip 判断 Accept-Encoding 是否含 gzip(带 q 值解析,q<=0 视为不接受)。
// acceptsGzip reports whether Accept-Encoding includes gzip (q-values honored;
// q<=0 counts as not accepted).
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		token := strings.TrimSpace(fields[0])
		if !strings.EqualFold(token, "gzip") {
			continue
		}
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(param, "q=") {
				if q, err := strconv.ParseFloat(strings.TrimPrefix(param, "q="), 64); err == nil && q <= 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

// bodyAllowed 报告状态码是否允许携带响应体(204/304/1xx 不允许)。
// bodyAllowed reports whether a status code permits a response body (204/304/1xx
// do not).
func bodyAllowed(status int) bool {
	return status >= http.StatusOK && status != http.StatusNoContent && status != http.StatusNotModified
}

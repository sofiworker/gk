package ghttp

import (
	"compress/gzip"
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// 压缩相关常量。
// Compression related constants.
const (
	// GzipDefaultMinSize 是默认的最小压缩阈值（字节）。
	// GzipDefaultMinSize is the default minimum size (bytes) for compression.
	GzipDefaultMinSize = 1024

	gzEncoding = "gzip"
)

// gzDefaultTypes 是默认可压缩的媒体类型；以 "/" 结尾表示前缀匹配，以 "+" 开头表示结构化后缀匹配。
// gzDefaultTypes lists compressible media types by default; entries ending with "/" are
// prefixes and entries starting with "+" are structured-syntax suffixes.
var gzDefaultTypes = []string{
	"text/",
	"application/json",
	"application/xml",
	"application/javascript",
	"application/x-javascript",
	"application/ecmascript",
	"application/xhtml+xml",
	"application/rss+xml",
	"application/atom+xml",
	"image/svg+xml",
	"+json",
	"+xml",
}

// gzConfig 是 Gzip 的配置。
// gzConfig is the Gzip configuration.
type gzConfig struct {
	level   int
	minSize int
	types   []string
}

// GzipOption 配置 Gzip。
// GzipOption configures Gzip.
type GzipOption func(*gzConfig)

// WithGzipLevel 设置压缩级别（gzip.HuffmanOnly..gzip.BestCompression）；非法值回退为默认级别。
// WithGzipLevel sets the compression level (gzip.HuffmanOnly..gzip.BestCompression);
// invalid values fall back to the default level.
func WithGzipLevel(level int) GzipOption {
	return func(c *gzConfig) { c.level = level }
}

// WithGzipMinSize 设置触发压缩的最小响应体大小（字节）；小于等于 0 表示不限制。
// WithGzipMinSize sets the minimum body size (bytes) to compress; <= 0 means no limit.
func WithGzipMinSize(n int) GzipOption {
	return func(c *gzConfig) { c.minSize = n }
}

// WithGzipContentTypes 替换可压缩的媒体类型列表。条目为精确类型（application/json）、
// 以 "/" 结尾的前缀（text/）或以 "+" 开头的后缀（+json）。
// WithGzipContentTypes replaces the compressible media type list. Entries are exact types
// (application/json), prefixes ending with "/" (text/), or suffixes starting with "+" (+json).
func WithGzipContentTypes(types ...string) GzipOption {
	return func(c *gzConfig) {
		c.types = make([]string, 0, len(types))
		for _, t := range types {
			t = strings.ToLower(strings.TrimSpace(t))
			t = strings.TrimSuffix(t, "*")
			if t != "" {
				c.types = append(c.types, t)
			}
		}
	}
}

// gzPools 按压缩级别（-2..9，偏移 2）缓存 gzip.Writer。
// gzPools caches gzip.Writers per level (-2..9, offset by 2).
var gzPools [12]sync.Pool

func gzPoolIndex(level int) int { return level - gzip.HuffmanOnly }

func gzGet(level int, w http.ResponseWriter) *gzip.Writer {
	p := &gzPools[gzPoolIndex(level)]
	if v, ok := p.Get().(*gzip.Writer); ok {
		v.Reset(w)
		return v
	}
	gw, _ := gzip.NewWriterLevel(w, level) // level 已校验 / level pre-validated
	return gw
}

func gzPut(level int, gw *gzip.Writer) {
	gw.Reset(nil)
	gzPools[gzPoolIndex(level)].Put(gw)
}

// Gzip 返回按 Accept-Encoding 协商的 gzip 响应压缩中间件。
// 跳过：客户端不接受 gzip、HEAD 与 Range 请求、无响应体状态码、已编码响应、Content-Range 响应、
// 不在白名单内的 Content-Type、小于阈值的响应。始终追加 Vary: Accept-Encoding。
// 压缩时强 ETag 会被降级为弱 ETag。
// Gzip returns a gzip compression middleware negotiated via Accept-Encoding. It skips
// clients not accepting gzip, HEAD and Range requests, bodiless statuses, already encoded
// responses, Content-Range responses, non-whitelisted Content-Types and responses below
// the threshold. Vary: Accept-Encoding is always added. Strong ETags are weakened when
// compressing.
func Gzip(opts ...GzipOption) Middleware {
	cfg := &gzConfig{level: gzip.DefaultCompression, minSize: GzipDefaultMinSize, types: gzDefaultTypes}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.level < gzip.HuffmanOnly || cfg.level > gzip.BestCompression {
		cfg.level = gzip.DefaultCompression
	}
	c := *cfg

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			gzAddVary(resp.Header())
			r := req.Raw
			if r.Method == http.MethodHead || r.Header.Get("Range") != "" ||
				!gzAcceptsGzip(r.Header.Values("Accept-Encoding")) {
				return next(ctx, req, resp)
			}
			orig := resp.Writer
			gw := &gzWriter{orig: orig, cfg: &c}
			resp.Writer = gw
			defer func() {
				gw.close()
				resp.Writer = orig
			}()
			return next(ctx, req, resp)
		}
	}
}

// gzAddVary 幂等地追加 Vary: Accept-Encoding。
// gzAddVary idempotently adds Vary: Accept-Encoding.
func gzAddVary(h http.Header) {
	for _, v := range h.Values("Vary") {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p == "*" || strings.EqualFold(p, "Accept-Encoding") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}

// gzAcceptsGzip 解析 Accept-Encoding（含 q 值），判断是否接受 gzip。
// gzAcceptsGzip parses Accept-Encoding (including q values) and reports whether gzip is accepted.
func gzAcceptsGzip(values []string) bool {
	var star, starSet, gz, gzSet bool
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			fields := strings.Split(part, ";")
			name := strings.ToLower(strings.TrimSpace(fields[0]))
			if name == "" {
				continue
			}
			q := 1.0
			for _, p := range fields[1:] {
				p = strings.TrimSpace(p)
				if len(p) > 2 && (p[0] == 'q' || p[0] == 'Q') && p[1] == '=' {
					f, err := strconv.ParseFloat(strings.TrimSpace(p[2:]), 64)
					if err != nil {
						f = 0
					}
					q = f
				}
			}
			switch name {
			case "gzip", "x-gzip":
				gz, gzSet = q > 0, true
			case "*":
				star, starSet = q > 0, true
			}
		}
	}
	if gzSet {
		return gz
	}
	return starSet && star
}

// gzTypeOK 判断媒体类型是否可压缩。
// gzTypeOK reports whether the media type is compressible.
func gzTypeOK(types []string, ct string) bool {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return false
	}
	for _, t := range types {
		switch {
		case strings.HasPrefix(t, "+"):
			if strings.HasSuffix(ct, t) {
				return true
			}
		case strings.HasSuffix(t, "/"):
			if strings.HasPrefix(ct, t) {
				return true
			}
		case ct == t:
			return true
		}
	}
	return false
}

// gzWriter 是延迟决定是否压缩的 ResponseWriter。
// gzWriter is a ResponseWriter that decides lazily whether to compress.
type gzWriter struct {
	orig http.ResponseWriter
	cfg  *gzConfig

	status    int
	hasStatus bool
	decided   bool
	compress  bool
	buf       []byte
	gz        *gzip.Writer
}

var (
	_ http.ResponseWriter = (*gzWriter)(nil)
	_ http.Flusher        = (*gzWriter)(nil)
)

// Header 返回底层响应头。
// Header returns the underlying headers.
func (w *gzWriter) Header() http.Header { return w.orig.Header() }

// Unwrap 返回底层写入器。
// Unwrap returns the underlying writer.
func (w *gzWriter) Unwrap() http.ResponseWriter { return w.orig }

// WriteHeader 记录状态码，实际写出延迟到决定是否压缩之后；1xx 信息响应直接透传。
// WriteHeader records the status; the real write is deferred until the compression decision.
// 1xx informational responses pass through.
func (w *gzWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.orig.WriteHeader(code)
		return
	}
	if w.hasStatus || w.decided {
		return
	}
	w.status, w.hasStatus = code, true
}

func (w *gzWriter) bodyless() bool {
	return w.status == http.StatusNoContent || w.status == http.StatusNotModified ||
		w.status < 200 || w.status == http.StatusSwitchingProtocols
}

// prepare 在首次写入/刷新时做能提前确定的决定；返回后若 decided 为 false 则需缓冲。
// prepare makes the decisions that can be made up front; if decided is still false the
// caller must buffer.
func (w *gzWriter) prepare() {
	if !w.hasStatus {
		w.status, w.hasStatus = http.StatusOK, true
	}
	h := w.orig.Header()
	if w.bodyless() || h.Get("Content-Range") != "" || w.status == http.StatusPartialContent {
		w.commit(false)
		return
	}
	if ce := h.Get("Content-Encoding"); ce != "" && !strings.EqualFold(ce, "identity") {
		w.commit(false)
		return
	}
	ct := h.Get("Content-Type")
	if ct != "" && !gzTypeOK(w.cfg.types, ct) {
		w.commit(false)
		return
	}
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
			if n < int64(w.cfg.minSize) {
				w.commit(false)
				return
			}
			if ct != "" {
				w.commit(true)
			}
		}
	}
}

// commit 固定决定并写出响应头与已缓冲数据。
// commit fixes the decision and writes headers and buffered data.
func (w *gzWriter) commit(compress bool) {
	w.decided, w.compress = true, compress
	h := w.orig.Header()
	gzAddVary(h)
	if w.compress && h.Get("Content-Type") == "" {
		// 压缩后无法再嗅探，先按原始内容确定类型 / cannot sniff after compression
		ct := http.DetectContentType(w.buf)
		if gzTypeOK(w.cfg.types, ct) {
			h.Set("Content-Type", ct)
		} else {
			w.compress = false
		}
	}
	if w.compress {
		h.Del("Content-Length")
		h.Set("Content-Encoding", gzEncoding)
		if et := h.Get("Etag"); et != "" && !strings.HasPrefix(et, "W/") {
			h.Set("Etag", "W/"+et)
		}
	}
	w.orig.WriteHeader(w.status)
	if w.compress {
		w.gz = gzGet(w.cfg.level, w.orig)
	}
	if len(w.buf) > 0 {
		b := w.buf
		w.buf = nil
		_, _ = w.out(b)
	}
}

func (w *gzWriter) out(p []byte) (int, error) {
	if w.compress {
		return w.gz.Write(p)
	}
	return w.orig.Write(p)
}

// Write 写入响应体：未决定时先缓冲，累计达到阈值后开始压缩。
// Write writes the body: buffers until decided, compressing once the threshold is reached.
func (w *gzWriter) Write(p []byte) (int, error) {
	if !w.decided {
		w.prepare()
		if !w.decided {
			w.buf = append(w.buf, p...)
			if len(w.buf) < w.cfg.minSize {
				return len(p), nil
			}
			w.commit(true)
			return len(p), nil
		}
	}
	return w.out(p)
}

// FlushError 立即把数据发送给客户端；压缩模式下先 Flush gzip.Writer。
// FlushError sends data to the client immediately; in compressed mode it flushes gzip first.
func (w *gzWriter) FlushError() error {
	if !w.decided {
		w.prepare()
		if !w.decided {
			// 流式场景：刷新即意味着不再等待阈值 / flushing means streaming, stop waiting for the threshold
			w.commit(len(w.buf) > 0 || w.orig.Header().Get("Content-Type") != "")
		}
	}
	if w.compress {
		if err := w.gz.Flush(); err != nil {
			return err
		}
	}
	return http.NewResponseController(w.orig).Flush()
}

// Flush 实现 http.Flusher。
// Flush implements http.Flusher.
func (w *gzWriter) Flush() { _ = w.FlushError() }

// close 结束写入：必要时提交缓冲，关闭 gzip.Writer 并归还池。
// close finishes writing: commits any buffer, closes the gzip.Writer and returns it to the pool.
func (w *gzWriter) close() {
	if !w.decided {
		if !w.hasStatus && len(w.buf) == 0 {
			return // 未写入任何内容 / nothing was written
		}
		w.prepare()
		if !w.decided {
			w.commit(len(w.buf) > 0 && len(w.buf) >= w.cfg.minSize)
		}
	}
	if w.gz != nil {
		_ = w.gz.Close()
		gzPut(w.cfg.level, w.gz)
		w.gz = nil
	}
}

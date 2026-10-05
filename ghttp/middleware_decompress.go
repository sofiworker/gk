package ghttp

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DecompressDefaultMaxBytes 是解压后请求体的默认上限（32 MiB）。
// DecompressDefaultMaxBytes is the default cap on the decompressed request body (32 MiB).
const DecompressDefaultMaxBytes int64 = 32 << 20

// decMaxLayers 是允许叠加的编码层数上限。
// decMaxLayers is the maximum number of stacked encodings accepted.
const decMaxLayers = 4

// decConfig 是 Decompress 的配置。
// decConfig is the Decompress configuration.
type decConfig struct {
	maxBytes int64
}

// DecompressOption 配置 Decompress。
// DecompressOption configures Decompress.
type DecompressOption func(*decConfig)

// WithDecompressMaxBytes 设置解压后字节数上限；<= 0 回退为默认值。超限时读取返回 *http.MaxBytesError（413）。
// WithDecompressMaxBytes sets the cap on decompressed bytes; <= 0 falls back to the default.
// Reads beyond it return *http.MaxBytesError (413).
func WithDecompressMaxBytes(n int64) DecompressOption {
	return func(c *decConfig) { c.maxBytes = n }
}

// Decompress 返回透明解压请求体的中间件，支持 gzip / x-gzip / deflate（zlib 封装，兼容裸 deflate）。
//
// 行为：命中编码时替换 req.Raw.Body，删除 Content-Encoding 与 Content-Length，ContentLength 置 -1；
// 无编码或 identity 原样透传；不支持的编码返回 415（并附 Accept-Encoding 提示）。
// 多重编码（如 "deflate, gzip"）按 RFC 9110 的应用顺序逆序解码，层数上限 4，超过返回 415。
// 解压是惰性的（首次读取时才开始），损坏的流在读取时返回包装 ErrInvalidInput 的错误（400），
// 底层读取错误（含上游 MaxBytesReader 的超限）原样透传。
//
// 应作为全局中间件（Server.Use）使用：路由级 body 上限（limitBody）包在路由中间件外层，
// 此时它限制的是压缩前的线上字节；解压后的体积由本中间件的 WithDecompressMaxBytes 防御解压炸弹。
//
// Decompress returns middleware that transparently decompresses request bodies (gzip, x-gzip,
// deflate as zlib, tolerating raw deflate).
//
// When an encoding matches, req.Raw.Body is replaced, Content-Encoding and Content-Length are
// removed and ContentLength becomes -1. No encoding / identity passes through; unsupported
// encodings yield 415 (with an Accept-Encoding hint). Stacked encodings (e.g. "deflate, gzip")
// are decoded in reverse order of application per RFC 9110, up to 4 layers (more yields 415).
// Decompression is lazy (starts on first Read); a corrupt stream yields an error wrapping
// ErrInvalidInput (400) on read, while underlying read errors (including an upstream
// MaxBytesReader overrun) pass through unchanged.
//
// Use it as GLOBAL middleware (Server.Use): the route-level body limit wraps outside route
// middleware and therefore bounds the compressed wire bytes; the decompressed size is bounded
// by WithDecompressMaxBytes, which defends against decompression bombs.
func Decompress(opts ...DecompressOption) Middleware {
	cfg := decConfig{maxBytes: DecompressDefaultMaxBytes}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.maxBytes <= 0 {
		cfg.maxBytes = DecompressDefaultMaxBytes
	}
	limit := cfg.maxBytes

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			r := req.Raw
			layers, err := decParseEncodings(r.Header.Values("Content-Encoding"))
			if err != nil {
				resp.Header().Set("Accept-Encoding", "gzip, deflate")
				return err
			}
			if len(layers) == 0 || r.Body == nil || r.Body == http.NoBody {
				return next(ctx, req, resp)
			}
			r.Body = &decBody{src: r.Body, layers: layers, max: limit}
			r.Header.Del("Content-Encoding")
			r.Header.Del("Content-Length")
			r.ContentLength = -1
			return next(ctx, req, resp)
		}
	}
}

// decParseEncodings 解析 Content-Encoding，返回需解码的层（按解码顺序，即逆序）；identity 被忽略。
// decParseEncodings parses Content-Encoding and returns the layers to decode, in decode order
// (reversed); identity is ignored.
func decParseEncodings(values []string) ([]string, error) {
	var applied []string
	for _, v := range values {
		for _, p := range strings.Split(v, ",") {
			p = strings.ToLower(strings.TrimSpace(p))
			switch p {
			case "", "identity":
			case "gzip", "x-gzip":
				applied = append(applied, "gzip")
			case "deflate":
				applied = append(applied, "deflate")
			default:
				return nil, HTTPError{
					Status:  http.StatusUnsupportedMediaType,
					Message: "unsupported content encoding",
				}
			}
		}
	}
	if len(applied) > decMaxLayers {
		return nil, HTTPError{Status: http.StatusUnsupportedMediaType, Message: "too many content encodings"}
	}
	for i, j := 0, len(applied)-1; i < j; i, j = i+1, j-1 {
		applied[i], applied[j] = applied[j], applied[i]
	}
	return applied, nil
}

// decSource 包装底层读取器，记录非 EOF 的底层错误以区分"数据损坏"与"传输错误"。
// decSource wraps a reader and records non-EOF errors to tell "corrupt data" from "transport error".
type decSource struct {
	r   io.Reader
	err error
}

func (s *decSource) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF {
		s.err = err
	}
	return n, err
}

// decBody 是惰性解压并限制解压后大小的请求体。
// decBody is a lazily decompressing, size-limited request body.
type decBody struct {
	src    io.ReadCloser
	layers []string
	max    int64

	srcErr  *decSource
	rd      io.Reader
	closers []io.Closer
	n       int64
	err     error
}

// init 构建解压链（首次读取时调用）。
// init builds the decoder chain (called on first read).
func (b *decBody) init() error {
	b.srcErr = &decSource{r: b.src}
	var cur io.Reader = b.srcErr
	for _, l := range b.layers {
		var (
			nr  io.Reader
			err error
		)
		switch l {
		case "gzip":
			var zr *gzip.Reader
			if zr, err = gzip.NewReader(cur); err == nil {
				b.closers = append(b.closers, zr)
				nr = zr
			}
		default:
			br := bufio.NewReader(cur)
			hdr, perr := br.Peek(2)
			if perr == nil && decIsZlibHeader(hdr[0], hdr[1]) {
				var zr io.ReadCloser
				if zr, err = zlib.NewReader(br); err == nil {
					b.closers = append(b.closers, zr)
					nr = zr
				}
			} else {
				fr := flate.NewReader(br)
				b.closers = append(b.closers, fr)
				nr = fr
			}
		}
		if err != nil {
			return err
		}
		cur = nr
	}
	b.rd = cur
	return nil
}

// decIsZlibHeader 判断前两字节是否为合法 zlib 头（deflate 方法，校验和通过）。
// decIsZlibHeader reports whether the two bytes form a valid zlib header.
func decIsZlibHeader(cmf, flg byte) bool {
	return cmf&0x0f == 8 && (uint16(cmf)<<8|uint16(flg))%31 == 0
}

// wrapErr 把解压错误归类：底层传输错误透传，其余视为损坏数据（400）。
// wrapErr classifies a decode error: transport errors pass through, the rest are corrupt data (400).
func (b *decBody) wrapErr(err error) error {
	if b.srcErr != nil && b.srcErr.err != nil {
		return b.srcErr.err
	}
	return fmt.Errorf("%w: corrupt compressed body: %v", ErrInvalidInput, err)
}

// Read 实现 io.Reader；解压后超过上限返回 *http.MaxBytesError。
// Read implements io.Reader; exceeding the decompressed cap returns *http.MaxBytesError.
func (b *decBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.rd == nil {
		if err := b.init(); err != nil {
			if errors.Is(err, io.EOF) && b.srcErr.err == nil {
				// 声明了编码但没有数据：视为损坏 / encoding declared but no data: corrupt
				err = io.ErrUnexpectedEOF
			}
			b.err = b.wrapErr(err)
			return 0, b.err
		}
	}
	if len(p) == 0 {
		return 0, nil
	}
	// 多读 1 字节用于探测是否超限 / read one extra byte to detect overrun
	if rem := b.max - b.n + 1; int64(len(p)) > rem {
		p = p[:rem]
	}
	n, err := b.rd.Read(p)
	if over := b.n + int64(n) - b.max; over > 0 {
		b.n = b.max
		b.err = &http.MaxBytesError{Limit: b.max}
		// Do not return a partial decoded value together with the limit error.
		// Decoders may otherwise consume the error and report a misleading syntax
		// error, losing the 413 classification.
		return 0, b.err
	}
	b.n += int64(n)
	if err != nil && err != io.EOF {
		b.err = b.wrapErr(err)
		return n, b.err
	}
	if err == io.EOF {
		b.err = io.EOF
	}
	return n, err
}

// Close 关闭解压器与底层请求体。
// Close closes the decoders and the underlying body.
func (b *decBody) Close() error {
	for _, c := range b.closers {
		_ = c.Close()
	}
	return b.src.Close()
}

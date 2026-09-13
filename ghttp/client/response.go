package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Response 是一次请求的响应快照：状态、头、体，以及在流式/内存两种模式之间的取舍结果。
//
// 两种模式：
//   - 内存（默认）：body 已被读入内存并受上限保护，Bytes/String/Decode/JSON/XML 都可用；
//   - 流式（SetStreamResponse）：body 仍是连接上的未读流，Decode 可流式解码，而
//     Bytes/String 返回 ErrStreamConsumed（不静默读一个已被消费的流）。
//
// Response is the snapshot of one response: status, headers, body, and the outcome of
// choosing between stream and in-memory modes.
//
// Two modes:
//   - memory (default): the body was read into memory under a size cap, so
//     Bytes/String/Decode/JSON/XML all work;
//   - stream (SetStreamResponse): the body is still an unread stream on the
//     connection, Decode can stream from it, and Bytes/String return
//     ErrStreamConsumed rather than silently reading a consumed stream.
type Response struct {
	request  *Request
	raw      *http.Response
	status   int
	header   http.Header
	duration time.Duration
	attempts int

	body       []byte
	bodyStream io.ReadCloser
	savedTo    string
	// trace 是 WithTrace 开启时采集的分阶段时序。
	// trace holds the phase timings collected when WithTrace is on.
	trace TraceInfo

	result any
	errTgt any
}

// StatusCode 返回 HTTP 状态码。
// StatusCode returns the HTTP status code.
func (r *Response) StatusCode() int { return r.status }

// Status 返回状态行文本（如 "200 OK"）。
// Status returns the status line text (e.g. "200 OK").
func (r *Response) Status() string {
	if r.raw == nil {
		return ""
	}
	return r.raw.Status
}

// Header 返回响应头。
// Header returns the response headers.
func (r *Response) Header() http.Header { return r.header }

// Cookies 返回响应设置的 Cookie。
// Cookies returns the cookies set by the response.
func (r *Response) Cookies() []*http.Cookie {
	if r.raw == nil {
		return nil
	}
	return r.raw.Cookies()
}

// Bytes 返回响应体字节。内存模式下返回内部切片（约定只读，不复制）；流式模式下返回
// ErrStreamConsumed。
// Bytes returns the response body bytes. In memory mode it returns the internal slice
// (treat it as read-only; no copy); in stream mode it returns ErrStreamConsumed.
func (r *Response) Bytes() ([]byte, error) {
	if r.bodyStream != nil {
		return nil, ErrStreamConsumed
	}
	return r.body, nil
}

// String 返回响应体文本，语义同 Bytes。
// String returns the body as text with the same semantics as Bytes.
func (r *Response) String() (string, error) {
	data, err := r.Bytes()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// IsSuccess 报告状态码是否在 2xx 范围内。
// IsSuccess reports whether the status is 2xx.
func (r *Response) IsSuccess() bool { return isSuccessStatus(r.status) }

// IsStream 报告本响应是否处于流式模式。
// IsStream reports whether this response is in stream mode.
func (r *Response) IsStream() bool { return r.bodyStream != nil }

// Body 返回响应体的读取器。内存模式下每次调用返回一个从头开始的新 reader，可重复读取；
// 流式模式下返回连接上的底层流，只应消费一次且【必须】Close（否则连接不会回池）。
// Body returns a reader over the response body. In memory mode each call returns a
// fresh reader from the start, so it can be read repeatedly; in stream mode it returns
// the underlying stream, which must be consumed at most once and MUST be Closed
// (otherwise the connection never returns to the pool).
func (r *Response) Body() io.ReadCloser {
	if r.bodyStream != nil {
		return r.bodyStream
	}
	return io.NopCloser(bytes.NewReader(r.body))
}

// Stream 返回流式模式的底层 body；内存模式下返回 ErrStreamConsumed。它是
// Body 的严格版本，用于明确表达"这里必须是流式"。
// Stream returns the underlying stream in stream mode and ErrStreamConsumed in memory
// mode. It is the strict form of Body, for expressing "this must be a stream".
func (r *Response) Stream() (io.ReadCloser, error) {
	if r.bodyStream == nil {
		return nil, ErrStreamConsumed
	}
	return r.bodyStream, nil
}

// Close 关闭流式模式的底层 body。内存模式下是空操作。
// Close closes the underlying body in stream mode; a no-op in memory mode.
func (r *Response) Close() error {
	if r.bodyStream != nil {
		err := r.bodyStream.Close()
		r.bodyStream = nil
		return err
	}
	return nil
}

// Decode 按响应的 Content-Type 选择已注册的解码器，把响应体解进 v（须为指针）。
// 内存模式从已缓存的字节解码，流式模式直接从连接解码。
// Decode picks the registered decoder for the response Content-Type and decodes the
// body into v (a pointer). Memory mode decodes the cached bytes; stream mode decodes
// straight from the connection.
func (r *Response) Decode(v any) error {
	ct := ""
	if r.header != nil {
		ct = r.header.Get("Content-Type")
	}
	cd, ok := lookupCodec(r.request.client.codecs, ct)
	if !ok || cd == nil {
		return fmt.Errorf("%w: %q", ErrNoCodec, mediaTypeOf(ct))
	}
	return r.decodeWith(cd, v)
}

// JSON 强制用 JSON 解码响应体，忽略 Content-Type。
// JSON forces JSON decoding of the body, ignoring the Content-Type.
func (r *Response) JSON(v any) error { return r.decodeWith(JSONCodec(), v) }

// XML 强制用 XML 解码响应体，忽略 Content-Type。
// XML forces XML decoding of the body, ignoring the Content-Type.
func (r *Response) XML(v any) error { return r.decodeWith(XMLCodec(), v) }

// decodeWith 用指定编解码器解码；流式模式不缓存、不重复读。
// decodeWith decodes with the given codec; stream mode neither caches nor re-reads.
func (r *Response) decodeWith(cd Codec, v any) error {
	if r.bodyStream != nil {
		if err := cd.Decode(r.bodyStream, v, contentLengthOf(r.raw)); err != nil {
			return fmt.Errorf("%w: %w", ErrDecode, err)
		}
		return nil
	}
	length := int64(len(r.body))
	if err := cd.Decode(bytes.NewReader(r.body), v, length); err != nil {
		return fmt.Errorf("%w: %w", ErrDecode, err)
	}
	return nil
}

// Raw 返回底层 *http.Response。流式模式下调用方若直接读它的 Body，需自行 Close。
// Raw returns the underlying *http.Response. If a caller reads its Body directly in
// stream mode, it must Close it.
func (r *Response) Raw() *http.Response { return r.raw }

// Request 返回产生本响应的请求。
// Request returns the request that produced this response.
func (r *Response) Request() *Request { return r.request }

// Duration 返回本次请求的墙钟耗时（含读取响应体）。
// Duration returns the wall-clock time of the request, including the body read.
func (r *Response) Duration() time.Duration { return r.duration }

// Attempts 返回总尝试次数（含第一次）。重试关闭时恒为 1。
// Attempts returns the total number of attempts including the first; always 1 with
// retries off.
func (r *Response) Attempts() int { return r.attempts }

// Result 返回 SetResult 注册的解码目标（已填充）；未注册时为 nil。
// Result returns the decode target registered by SetResult (already filled), or nil.
func (r *Response) Result() any { return r.result }

// ErrorBody 返回 SetError 注册的错误体解码目标（已填充）；未注册时为 nil。
// ErrorBody returns the error-body target registered by SetError (already filled), or
// nil.
func (r *Response) ErrorBody() any { return r.errTgt }

// SavedTo 返回 SetOutputFile 落盘的实际路径；未落盘时为空。
// SavedTo returns the path written by SetOutputFile, or empty when nothing was saved.
func (r *Response) SavedTo() string { return r.savedTo }

// SaveToFile 把响应体写入 path（权限 0o644）。内存模式下写缓存字节，流式模式下把剩余流
// 拷贝进文件并关闭它。写失败时会清理半成品文件，避免留下截断的下载结果被误用。
// SaveToFile writes the body to path (mode 0o644). In memory mode it writes the cached
// bytes; in stream mode it copies the remaining stream into the file and closes it. On
// failure the partial file is removed so a truncated download is never mistaken for a
// complete one.
func (r *Response) SaveToFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var werr error
	switch {
	case r.bodyStream != nil:
		// 流式模式：把剩余流拷完并关闭。
		// Stream mode: drain the remaining stream and close it.
		_, werr = io.Copy(f, r.bodyStream)
		if cerr := r.bodyStream.Close(); werr == nil {
			werr = cerr
		}
		r.bodyStream = nil
	case r.body == nil && r.raw != nil && r.raw.Body != nil:
		// 落盘模式（SetOutputFile）：body 尚未进内存，直接从连接拷贝，避免多一份内存副本。
		// File mode (SetOutputFile): the body was never buffered, so copy straight from
		// the connection instead of keeping a second in-memory copy.
		_, werr = io.Copy(f, r.raw.Body)
		if cerr := r.raw.Body.Close(); werr == nil {
			werr = cerr
		}
	default:
		_, werr = f.Write(r.body)
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return werr
	}
	r.savedTo = path
	return nil
}

// contentLengthOf 读取底层响应的声明长度；未知（chunked）返回 -1。
// contentLengthOf reads the declared body length; -1 when unknown (chunked).
func contentLengthOf(raw *http.Response) int64 {
	if raw == nil {
		return -1
	}
	return raw.ContentLength
}

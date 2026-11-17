package codec

import (
	"bytes"
	"strconv"
)

// maxHTTPHeaderSize 是 HTTP 头部分析上限（防恶意头）。
// maxHTTPHeaderSize caps header scanning (guards hostile headers).
const maxHTTPHeaderSize = 1 << 20

// HTTPFramer 按 HTTP/1.x 消息语义分帧：Content-Length 或 chunked 定界。
// 有状态：内部缓冲跨调用累积，Split 返回的 frame 可能引用内部缓冲，
// 须在下次调用前复制；单个实例不可跨连接复用。
//
// HTTPFramer frames HTTP/1.x messages by Content-Length or chunked framing.
// It is stateful: an internal buffer accumulates across calls, so returned
// frames may reference it and must be copied before the next call; an
// instance must not be reused across connections.
type HTTPFramer struct {
	buf []byte
}

// Encode 透传完整 HTTP 消息（不做改写）。
// Encode passes a complete HTTP message through unchanged.
func (HTTPFramer) Encode(msg []byte) ([]byte, error) {
	return msg, nil
}

// Split 累积数据并尝试切出完整消息。
// Split accumulates data and tries to slice one complete message.
func (f *HTTPFramer) Split(data []byte, atEOF bool) (int, []byte, int, error) {
	f.buf = append(f.buf, data...)
	adv := len(data)

	headEnd := bytes.Index(f.buf, []byte("\r\n\r\n"))
	if headEnd < 0 {
		if len(f.buf) > maxHTTPHeaderSize {
			return adv, nil, 0, ErrHeaderTooLarge
		}
		if atEOF && len(f.buf) > 0 {
			return adv, nil, 0, ErrUnexpectedEOF
		}
		return adv, nil, 0, nil
	}
	head := f.buf[:headEnd]
	bodyLen, chunked, err := parseHTTPHead(head)
	if err != nil {
		return adv, nil, 0, err
	}
	bodyStart := headEnd + 4

	if chunked {
		end, err := chunkedEnd(f.buf, bodyStart)
		if err != nil {
			if err == errNeedMore {
				if atEOF {
					return adv, nil, 0, ErrUnexpectedEOF
				}
				return adv, nil, 0, nil
			}
			return adv, nil, 0, err
		}
		msg := f.buf[:end]
		f.buf = f.buf[end:]
		return adv, msg, 0, nil
	}

	end := bodyStart + bodyLen
	if len(f.buf) < end {
		if atEOF {
			return adv, nil, 0, ErrUnexpectedEOF
		}
		return adv, nil, end, nil
	}
	msg := f.buf[:end]
	f.buf = f.buf[end:]
	return adv, msg, 0, nil
}

// parseHTTPHead 解析头部行，返回 (Content-Length, 是否 chunked)。
// parseHTTPHead parses the head lines, returning (Content-Length, chunked).
func parseHTTPHead(head []byte) (int, bool, error) {
	contentLength := -1
	chunked := false
	for _, line := range bytes.Split(head, []byte("\r\n")) {
		if len(line) == 0 {
			continue
		}
		i := bytes.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		name := string(line[:i])
		value := string(bytes.TrimSpace(line[i+1:]))
		switch {
		case equalFoldASCII(name, "Content-Length"):
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return 0, false, &httpFramingError{"invalid Content-Length: " + value}
			}
			contentLength = n
		case equalFoldASCII(name, "Transfer-Encoding"):
			chunked = chunked || hasToken(value, "chunked")
		}
	}
	if chunked {
		return 0, true, nil
	}
	if contentLength < 0 {
		// 无定界：按无请求体处理（消息止于头部空行）。
		// 关闭定界的响应体需上层在 EOF 语义下自行处理。
		// No framing: treat as bodyless (message ends at the head).
		// Close-delimited response bodies need EOF handling by the caller.
		return 0, false, nil
	}
	return contentLength, false, nil
}

// chunkedEnd 返回 chunked 体结束后整个消息的长度；不完整返回 errNeedMore。
// chunkedEnd returns the message length past the chunked body;
// errNeedMore when incomplete.
func chunkedEnd(buf []byte, start int) (int, error) {
	pos := start
	for {
		lineEnd := bytes.Index(buf[pos:], []byte("\r\n"))
		if lineEnd < 0 {
			return 0, errNeedMore
		}
		lineEnd += pos
		line := buf[pos:lineEnd]
		if i := bytes.IndexByte(line, ';'); i >= 0 {
			line = line[:i]
		}
		size, err := strconv.ParseUint(string(bytes.TrimSpace(line)), 16, 64)
		if err != nil {
			return 0, &httpFramingError{"invalid chunk size: " + string(line)}
		}
		pos = lineEnd + 2
		if size == 0 {
			// 空 trailer：直接一个 CRLF 结束；带 trailer 头则以空行结束。
			// Empty trailer ends with a bare CRLF; trailers end with a blank
			// line.
			if bytes.HasPrefix(buf[pos:], []byte("\r\n")) {
				return pos + 2, nil
			}
			trailerEnd := bytes.Index(buf[pos:], []byte("\r\n\r\n"))
			if trailerEnd < 0 {
				return 0, errNeedMore
			}
			return pos + trailerEnd + 4, nil
		}
		end := pos + int(size) + 2
		if len(buf) < end {
			return 0, errNeedMore
		}
		pos = end
	}
}

// equalFoldASCII 是大小写不敏感的 ASCII 字符串比较。
// equalFoldASCII compares ASCII strings case-insensitively.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// hasToken 判断逗号分隔列表中是否含 token。
// hasToken reports whether the comma-separated list contains token.
func hasToken(list, token string) bool {
	for _, part := range bytes.Split([]byte(list), []byte(",")) {
		if equalFoldASCII(string(bytes.TrimSpace(part)), token) {
			return true
		}
	}
	return false
}

// ErrHeaderTooLarge 表示 HTTP 头超过上限。
// ErrHeaderTooLarge means the HTTP head exceeds the cap.
var ErrHeaderTooLarge = &httpFramingError{"http header too large"}

// httpFramingError 是 HTTP 分帧错误。
// httpFramingError is an HTTP framing error.
type httpFramingError struct{ msg string }

func (e *httpFramingError) Error() string { return "codec: " + e.msg }

// errNeedMore 是 chunked 解析的内部“缺数据”信号。
// errNeedMore is the internal need-more-data signal for chunked parsing.
var errNeedMore = &httpFramingError{"need more"}

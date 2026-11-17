package codec

import (
	"bytes"
	"errors"
)

// ErrInvalidFixedLength 表示固定长度非法。
// ErrInvalidFixedLength means an invalid fixed length.
var ErrInvalidFixedLength = errors.New("codec: fixed length must be positive")

// DelimiterFramer 按分隔字节分帧：frame 为分隔符前的载荷（不含分隔符），
// adv 包含分隔符本身。Encode 在载荷后追加分隔符。
//
// DelimiterFramer frames by a delimiter byte: frame is the payload before the
// delimiter (delimiter excluded), adv includes the delimiter. Encode appends
// the delimiter.
type DelimiterFramer struct {
	Delim byte
}

// Encode 追加分隔符。
// Encode appends the delimiter.
func (f DelimiterFramer) Encode(msg []byte) ([]byte, error) {
	buf := make([]byte, len(msg)+1)
	copy(buf, msg)
	buf[len(msg)] = f.Delim
	return buf, nil
}

// Split 按分隔符切分。
// Split splits on the delimiter.
func (f DelimiterFramer) Split(data []byte, atEOF bool) (int, []byte, int, error) {
	if i := bytes.IndexByte(data, f.Delim); i >= 0 {
		return i + 1, data[:i], 0, nil
	}
	if atEOF {
		if len(data) == 0 {
			return 0, nil, 0, nil
		}
		// 末尾无分隔符：整个剩余数据视为最终帧。
		return len(data), data, 0, nil
	}
	return 0, nil, 0, nil
}

// LineFramer 按行分帧：以 \n 结尾，行内容剥去尾部 \r。
// Encode 以 \r\n 结尾。
//
// LineFramer frames lines terminated by \n with a trailing \r stripped.
// Encode terminates with \r\n.
type LineFramer struct{}

// Encode 以 \r\n 结尾。
// Encode terminates with \r\n.
func (LineFramer) Encode(msg []byte) ([]byte, error) {
	buf := make([]byte, len(msg)+2)
	copy(buf, msg)
	buf[len(msg)], buf[len(msg)+1] = '\r', '\n'
	return buf, nil
}

// Split 按行切分。
// Split splits lines.
func (LineFramer) Split(data []byte, atEOF bool) (int, []byte, int, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		line := data[:i]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		return i + 1, line, 0, nil
	}
	if atEOF {
		if len(data) == 0 {
			return 0, nil, 0, nil
		}
		line := data
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		return len(data), line, 0, nil
	}
	return 0, nil, 0, nil
}

// FixedLengthFramer 按固定长度分帧：Encode 截断或零填充到 N 字节。
//
// FixedLengthFramer frames by a fixed size: Encode truncates or zero-pads to
// N bytes.
type FixedLengthFramer struct {
	N int
}

// Encode 截断或填充到 N 字节。
// Encode truncates or pads to N bytes.
func (f FixedLengthFramer) Encode(msg []byte) ([]byte, error) {
	if f.N <= 0 {
		return nil, ErrInvalidFixedLength
	}
	buf := make([]byte, f.N)
	copy(buf, msg)
	return buf, nil
}

// Split 按 N 字节切分。
// Split splits every N bytes.
func (f FixedLengthFramer) Split(data []byte, atEOF bool) (int, []byte, int, error) {
	if f.N <= 0 {
		return 0, nil, 0, ErrInvalidFixedLength
	}
	if len(data) < f.N {
		if atEOF {
			return 0, nil, 0, ErrUnexpectedEOF
		}
		return 0, nil, f.N, nil
	}
	return f.N, data[:f.N], 0, nil
}

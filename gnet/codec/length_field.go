package codec

import (
	"encoding/binary"
	"fmt"
)

// LengthFieldFramer 按长度字段分帧，参数语义与 Netty
// LengthFieldBasedFrameDecoder 对齐：
//
//	frameLen = LengthFieldOffset + LengthFieldLength + 长度字段值 + LengthAdjustment
//
// Encode 产出 [offset 个零字节][长度字段(大端)][msg]，长度字段值 =
// len(msg) + LengthAdjustment，保证 Split 对称解出 msg。
//
// LengthFieldFramer frames by a length field with Netty
// LengthFieldBasedFrameDecoder-compatible parameters: frameLen = offset +
// lengthFieldLength + lengthValue + adjustment. Encode produces
// [offset zero bytes][length field (big endian)][msg] with lengthValue =
// len(msg) + adjustment, so Split decodes msg symmetrically.
type LengthFieldFramer struct {
	// LengthFieldOffset 长度字段相对帧头的偏移。
	LengthFieldOffset int
	// LengthFieldLength 长度字段字节数（1/2/4/8）。
	LengthFieldLength int
	// LengthAdjustment 长度字段值之外的修正量（可负）。
	LengthAdjustment int
	// InitialBytesToStrip 解码后从帧头剥去的字节数。
	InitialBytesToStrip int
	// MaxFrameLength 最大帧长（0 表示不限制）。
	MaxFrameLength int
}

// Encode 组帧。长度字段写入 len(msg)（不含 LengthAdjustment——
// adjustment 是解码侧对帧长的修正，Netty 语义）。
// Encode frames a message. The length field carries len(msg) without
// LengthAdjustment (the adjustment is a decode-side correction, per Netty).
func (f LengthFieldFramer) Encode(msg []byte) ([]byte, error) {
	if err := checkLengthField(f.LengthFieldLength); err != nil {
		return nil, err
	}
	buf := make([]byte, f.LengthFieldOffset+f.LengthFieldLength+len(msg))
	if err := putLength(buf[f.LengthFieldOffset:], f.LengthFieldLength, uint64(len(msg))); err != nil {
		return nil, err
	}
	copy(buf[f.LengthFieldOffset+f.LengthFieldLength:], msg)
	return buf, nil
}

// Split 分帧。
// Split splits a frame.
func (f LengthFieldFramer) Split(data []byte, atEOF bool) (int, []byte, int, error) {
	headerLen := f.LengthFieldOffset + f.LengthFieldLength
	if len(data) < headerLen {
		return 0, nil, headerLen, nil
	}
	length, err := getLength(data[f.LengthFieldOffset:], f.LengthFieldLength)
	if err != nil {
		return 0, nil, 0, err
	}
	frameLen := headerLen + int(length) + f.LengthAdjustment
	if frameLen < headerLen {
		return 0, nil, 0, fmt.Errorf("codec: negative frame length %d", frameLen)
	}
	if f.MaxFrameLength > 0 && frameLen > f.MaxFrameLength {
		return 0, nil, 0, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, frameLen, f.MaxFrameLength)
	}
	if len(data) < frameLen {
		if atEOF {
			return 0, nil, 0, ErrUnexpectedEOF
		}
		return 0, nil, frameLen, nil
	}
	frame := data[:frameLen]
	if strip := f.InitialBytesToStrip; strip > 0 {
		if strip >= frameLen {
			strip = frameLen
		}
		frame = frame[strip:]
	}
	return frameLen, frame, 0, nil
}

// checkLengthField 校验长度字段宽度。
// checkLengthField validates the length-field width.
func checkLengthField(n int) error {
	switch n {
	case 1, 2, 4, 8:
		return nil
	}
	return fmt.Errorf("codec: invalid length field width %d (want 1/2/4/8)", n)
}

// putLength 写入大端长度字段。
// putLength writes the big-endian length field.
func putLength(dst []byte, width int, v uint64) error {
	switch width {
	case 1:
		if v > 0xff {
			return fmt.Errorf("codec: length %d overflows 1-byte field", v)
		}
		dst[0] = byte(v)
	case 2:
		if v > 0xffff {
			return fmt.Errorf("codec: length %d overflows 2-byte field", v)
		}
		binary.BigEndian.PutUint16(dst, uint16(v))
	case 4:
		if v > 0xffffffff {
			return fmt.Errorf("codec: length %d overflows 4-byte field", v)
		}
		binary.BigEndian.PutUint32(dst, uint32(v))
	case 8:
		binary.BigEndian.PutUint64(dst, v)
	}
	return nil
}

// getLength 读取大端长度字段。
// getLength reads the big-endian length field.
func getLength(src []byte, width int) (uint64, error) {
	if err := checkLengthField(width); err != nil {
		return 0, err
	}
	if len(src) < width {
		return 0, fmt.Errorf("codec: short length field")
	}
	switch width {
	case 1:
		return uint64(src[0]), nil
	case 2:
		return uint64(binary.BigEndian.Uint16(src)), nil
	case 4:
		return uint64(binary.BigEndian.Uint32(src)), nil
	default:
		return binary.BigEndian.Uint64(src), nil
	}
}

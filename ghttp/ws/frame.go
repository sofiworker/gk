package ws

import (
	"bufio"
	"encoding/binary"
	"math"
	"unicode/utf8"
)

// 帧编解码原语，与传输方向无关，供服务端与未来的客户端共用。
// Frame codec primitives, direction-agnostic, shared by the server and a future client.

// opcode 常量（RFC 6455 §5.2）。
// Opcode constants (RFC 6455 §5.2).
const (
	opContinuation byte = 0x0
	opText         byte = 0x1
	opBinary       byte = 0x2
	opClose        byte = 0x8
	opPing         byte = 0x9
	opPong         byte = 0xA
)

// 帧头首字节与第二字节的位掩码。
// Bit masks of the first and second header bytes.
const (
	finBit     byte = 0x80
	rsvBits    byte = 0x70
	opcodeBits byte = 0x0F
	maskBit    byte = 0x80
	len7Bits   byte = 0x7F
)

// maxFrameHeaderSize 是帧头最大字节数：2 + 8（扩展长度）+ 4（掩码键）。
// maxFrameHeaderSize is the maximum header size: 2 + 8 (extended length) + 4 (mask key).
const maxFrameHeaderSize = 14

// frameHeader 是解析后的帧头。
// frameHeader is a parsed frame header.
type frameHeader struct {
	fin    bool
	rsv    byte
	opcode byte
	masked bool
	length int64
	key    [4]byte
}

// isControlOpcode 报告 op 是否为控制帧 opcode（最高位为 1）。
// isControlOpcode reports whether op is a control opcode (high bit set).
func isControlOpcode(op byte) bool { return op&0x8 != 0 }

// isKnownOpcode 报告 op 是否为 RFC 6455 定义的 opcode。
// isKnownOpcode reports whether op is an opcode defined by RFC 6455.
func isKnownOpcode(op byte) bool {
	switch op {
	case opContinuation, opText, opBinary, opClose, opPing, opPong:
		return true
	}
	return false
}

// errFrameLength 表示 64 位负载长度的最高位非零。
// errFrameLength reports a 64-bit payload length with the most significant bit set.
var errFrameLength = &failError{code: CloseProtocolError, reason: "invalid payload length", kind: ErrProtocol}

// readFrameHeader 从 br 读取一个帧头，不做语义校验（RSV、opcode、掩码方向由调用方检查）。
// 使用 ReadByte/Peek 避免分配。
// readFrameHeader reads one frame header from br without semantic checks (RSV, opcode
// and mask direction are checked by the caller). It uses ReadByte/Peek to avoid
// allocations.
func readFrameHeader(br *bufio.Reader) (frameHeader, error) {
	var h frameHeader
	b0, err := br.ReadByte()
	if err != nil {
		return h, err
	}
	b1, err := br.ReadByte()
	if err != nil {
		return h, err
	}
	h.fin = b0&finBit != 0
	h.rsv = b0 & rsvBits
	h.opcode = b0 & opcodeBits
	h.masked = b1&maskBit != 0

	switch n := b1 & len7Bits; n {
	case 126:
		p, err := peekDiscard(br, 2)
		if err != nil {
			return h, err
		}
		h.length = int64(binary.BigEndian.Uint16(p))
	case 127:
		p, err := peekDiscard(br, 8)
		if err != nil {
			return h, err
		}
		v := binary.BigEndian.Uint64(p)
		if v > math.MaxInt64 {
			return h, errFrameLength
		}
		h.length = int64(v)
	default:
		h.length = int64(n)
	}

	if h.masked {
		p, err := peekDiscard(br, 4)
		if err != nil {
			return h, err
		}
		copy(h.key[:], p)
	}
	return h, nil
}

// peekDiscard 读取并消费 n 字节（n 不超过 bufio 最小缓冲 16）。返回的切片在下次读取前有效。
// peekDiscard reads and consumes n bytes (n must not exceed bufio's minimum buffer of
// 16). The returned slice is valid until the next read.
func peekDiscard(br *bufio.Reader, n int) ([]byte, error) {
	p, err := br.Peek(n)
	if err != nil {
		return nil, err
	}
	_, _ = br.Discard(n) // 已 Peek 到 n 字节，不会失败 / n bytes were peeked, cannot fail
	return p, nil
}

// appendFrameHeader 把帧头追加到 dst。masked 为 true 时写入掩码位与 key。
// appendFrameHeader appends a frame header to dst, including the mask bit and key when
// masked is true.
func appendFrameHeader(dst []byte, fin bool, opcode byte, length int, masked bool, key [4]byte) []byte {
	b0 := opcode
	if fin {
		b0 |= finBit
	}
	var b1 byte
	if masked {
		b1 = maskBit
	}
	switch {
	case length <= 125:
		dst = append(dst, b0, b1|byte(length))
	case length <= math.MaxUint16:
		dst = append(dst, b0, b1|126)
		dst = binary.BigEndian.AppendUint16(dst, uint16(length))
	default:
		dst = append(dst, b0, b1|127)
		dst = binary.BigEndian.AppendUint64(dst, uint64(length))
	}
	if masked {
		dst = append(dst, key[:]...)
	}
	return dst
}

// maskBytes 用 key 从偏移 pos 开始对 b 原地异或，返回下一个偏移（0..3）。
// 掩码与解掩码是同一操作。长切片按 8 字节一组处理。
// maskBytes XORs b in place with key starting at offset pos and returns the next offset
// (0..3). Masking and unmasking are the same operation. Long slices are processed in
// 8-byte words.
func maskBytes(key [4]byte, pos int, b []byte) int {
	if len(b) >= 16 {
		var k [8]byte
		for i := range k {
			k[i] = key[(pos+i)&3]
		}
		kw := binary.LittleEndian.Uint64(k[:])
		n := len(b) &^ 7
		for i := 0; i < n; i += 8 {
			binary.LittleEndian.PutUint64(b[i:], binary.LittleEndian.Uint64(b[i:])^kw)
		}
		for i := n; i < len(b); i++ {
			b[i] ^= key[(pos+i)&3]
		}
		return (pos + len(b)) & 3
	}
	for i := range b {
		b[i] ^= key[(pos+i)&3]
	}
	return (pos + len(b)) & 3
}

// utf8Validator 增量校验分块到达的 UTF-8 数据，允许多字节字符跨块。
// utf8Validator incrementally validates UTF-8 data arriving in chunks, allowing
// multi-byte characters to span chunks.
type utf8Validator struct {
	buf [utf8.UTFMax]byte
	n   int
}

// write 校验下一块数据；发现非法序列时返回 false。
// write validates the next chunk and returns false on an invalid sequence.
func (v *utf8Validator) write(p []byte) bool {
	for v.n > 0 && len(p) > 0 {
		v.buf[v.n] = p[0]
		v.n++
		p = p[1:]
		if utf8.FullRune(v.buf[:v.n]) {
			r, size := utf8.DecodeRune(v.buf[:v.n])
			if r == utf8.RuneError && size <= 1 {
				return false
			}
			v.n = 0
		}
	}
	if len(p) == 0 {
		return true
	}
	tail := incompleteTail(p)
	if !utf8.Valid(p[:len(p)-tail]) {
		return false
	}
	v.n = copy(v.buf[:], p[len(p)-tail:])
	return true
}

// done 报告数据是否在字符边界结束；不论结果都会重置状态。
// done reports whether the data ended on a character boundary; it resets the state.
func (v *utf8Validator) done() bool {
	ok := v.n == 0
	v.n = 0
	return ok
}

// incompleteTail 返回 p 末尾不完整（但可能合法）的多字节字符前缀长度。
// incompleteTail returns the length of an incomplete (possibly valid) multi-byte
// character prefix at the end of p.
func incompleteTail(p []byte) int {
	for i := len(p) - 1; i >= 0 && i >= len(p)-(utf8.UTFMax-1); i-- {
		if utf8.RuneStart(p[i]) {
			if utf8.FullRune(p[i:]) {
				return 0
			}
			return len(p) - i
		}
	}
	return 0
}

// validReceivedCloseCode 报告 code 是否可以出现在收到的关闭帧中（RFC 6455 §7.4）。
// validReceivedCloseCode reports whether code may appear in a received close frame
// (RFC 6455 §7.4).
func validReceivedCloseCode(code int) bool {
	switch {
	case code >= 1000 && code <= 1003:
		return true
	case code >= 1007 && code <= 1014:
		return true
	case code >= 3000 && code <= 4999:
		return true
	}
	return false
}

// parseClosePayload 解析关闭帧负载。无负载时返回 CloseNoStatusReceived；
// 非法负载返回 *failError。
// parseClosePayload parses a close frame payload. An empty payload yields
// CloseNoStatusReceived; an invalid payload yields a *failError.
func parseClosePayload(p []byte) (int, string, *failError) {
	switch {
	case len(p) == 0:
		return CloseNoStatusReceived, "", nil
	case len(p) == 1:
		return 0, "", &failError{code: CloseProtocolError, reason: "close payload of 1 byte", kind: ErrProtocol}
	}
	code := int(binary.BigEndian.Uint16(p))
	if !validReceivedCloseCode(code) {
		return 0, "", &failError{code: CloseProtocolError, reason: "invalid close code", kind: ErrProtocol}
	}
	if !utf8.Valid(p[2:]) {
		return 0, "", &failError{code: CloseInvalidFramePayloadData, reason: "invalid UTF-8 in close reason", kind: ErrInvalidUTF8}
	}
	return code, string(p[2:]), nil
}

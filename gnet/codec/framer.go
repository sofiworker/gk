// Package codec 提供流式分帧编解码：无状态 Framer 链（对齐
// bufio.SplitFunc 语义）与内置分帧器。设计见
// docs/superpowers/specs/2026-08-16-gnet-network-foundation-redesign.md §5.4。
//
// Package codec provides stream framing codecs: a stateless Framer chain
// (aligned with bufio.SplitFunc semantics) plus built-in framers. See the
// gnet redesign spec §5.4.
package codec

import "errors"

// ErrFrameTooLarge 表示帧长超过 MaxFrameLength。
// ErrFrameTooLarge means the frame exceeds MaxFrameLength.
var ErrFrameTooLarge = errors.New("codec: frame too large")

// ErrUnexpectedEOF 表示 atEOF 时仍有不完整帧。
// ErrUnexpectedEOF means an incomplete frame remains at EOF.
var ErrUnexpectedEOF = errors.New("codec: unexpected EOF in frame")

// Framer 是分帧器：出站组帧（Encode），入站分帧（Split）。
//
// Split 约定：
//   - adv：本次从 data 中消费的字节数；
//   - frame：完整帧字节。无状态分帧器返回 data 内的零拷贝切片；
//     有状态分帧器（如 HTTPFramer）可能返回内部缓冲，须在下次调用前复制；
//   - need>0：半包，还需至少 need 字节才能完成当前帧；
//   - frame==nil 且 need==0 且 err==nil：需要更多数据但无法预估字节数
//     （调用方继续喂入数据即可）；
//   - atEOF 且帧不完整：返回 ErrUnexpectedEOF。
//
// Framer is a codec: outbound framing (Encode) and inbound splitting (Split).
// Split contract: adv is the bytes consumed from data; frame is the complete
// frame (zero-copy slices of data for stateless framers, possibly internal
// buffers for stateful ones such as HTTPFramer — copy before the next call);
// need>0 signals a partial frame requiring at least that many more bytes;
// frame==nil && need==0 && err==nil means more data is required with an
// unpredictable byte count; an incomplete frame at EOF yields ErrUnexpectedEOF.
type Framer interface {
	Encode(msg []byte) ([]byte, error)
	Split(data []byte, atEOF bool) (adv int, frame []byte, need int, err error)
}

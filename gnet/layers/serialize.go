package layers

import (
	"encoding/binary"
	"fmt"
)

// SerializeOptions 控制序列化行为。
// SerializeOptions controls serialization behavior.
type SerializeOptions struct {
	// FixLengths 自动修正 IPv4 TotalLength / UDP Length 等长度字段。
	FixLengths bool
	// ComputeChecksums 自动计算 IPv4 头、ICMP、TCP/UDP（伪头）校验和。
	ComputeChecksums bool
}

// layerRec 记录一层及其在最终缓冲中的起点。
// layerRec records a layer and its start offset in the final buffer.
type layerRec struct {
	enc   Encoder
	start int
}

// SerializeBuffer 是构造报文的可增长缓冲：层由内向外依次前插，
// 支持统一长度修正与校验和计算。
//
// SerializeBuffer is a growable buffer for crafting packets: layers are
// prepended innermost-first, with unified length fixing and checksums.
type SerializeBuffer struct {
	opts   SerializeOptions
	buf    []byte
	layers []layerRec
}

// NewSerializeBuffer 创建序列化缓冲。
// NewSerializeBuffer creates a serialization buffer.
func NewSerializeBuffer(opts ...SerializeOptions) *SerializeBuffer {
	o := SerializeOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	return &SerializeBuffer{opts: o}
}

// PrependHeader 在现有内容前预留 n 字节并记录层起点。
// 各层 SerializeTo 用它写自己的头部。
//
// PrependHeader reserves n bytes before the current content and records the
// layer start. Each layer's SerializeTo uses it to write its header.
func (b *SerializeBuffer) PrependHeader(n int, enc Encoder) []byte {
	for i := range b.layers {
		b.layers[i].start += n
	}
	b.layers = append([]layerRec{{enc: enc, start: 0}}, b.layers...)
	old := b.buf
	b.buf = make([]byte, 0, len(old)+n)
	b.buf = append(b.buf, make([]byte, n)...)
	b.buf = append(b.buf, old...)
	return b.buf[:n]
}

// Bytes 返回完整报文。
// Bytes returns the complete packet.
func (b *SerializeBuffer) Bytes() []byte {
	return b.buf
}

// Encoder 是可序列化的层。
// Encoder is a serializable layer.
type Encoder interface {
	LayerType() LayerType
	// SerializeTo 用 PrependHeader 前插本层头部。
	// SerializeTo prepends this layer's header via PrependHeader.
	SerializeTo(b *SerializeBuffer) error
}

// Payload 是原始负载层（序列化时原样前插）。
// Payload is a raw payload layer (prepended verbatim).
type Payload []byte

// LayerType 返回 LayerTypePayload。
// LayerType returns LayerTypePayload.
func (p Payload) LayerType() LayerType { return LayerTypePayload }

// SerializeTo 原样前插。
// SerializeTo prepends verbatim.
func (p Payload) SerializeTo(b *SerializeBuffer) error {
	dst := b.PrependHeader(len(p), p)
	copy(dst, p)
	return nil
}

// Length 实现 Layer。
// Length implements Layer.
func (p Payload) Length() int { return len(p) }

// Payload 实现 Layer（负载层无内部负载）。
// Payload implements Layer (a payload layer has no inner payload).
func (p Payload) Payload() []byte { return nil }

// String 实现 Layer。
// String implements Layer.
func (p Payload) String() string { return fmt.Sprintf("Payload(%d bytes)", len(p)) }

// SerializeLayers 按“外→内”列出各层并序列化（内部从内向外前插），
// 随后按选项统一修正长度与校验和。全部层须实现 Encoder。
//
// SerializeLayers serializes layers listed outermost-first (prepended
// innermost-first internally), then fixes lengths and checksums per the
// options. Every layer must implement Encoder.
func SerializeLayers(opts SerializeOptions, layers ...Layer) ([]byte, error) {
	b := NewSerializeBuffer(opts)
	for i := len(layers) - 1; i >= 0; i-- {
		enc, ok := layers[i].(Encoder)
		if !ok {
			return nil, fmt.Errorf("layers: layer %T does not implement Encoder", layers[i])
		}
		if err := enc.SerializeTo(b); err != nil {
			return nil, err
		}
	}
	b.fixup()
	return b.Bytes(), nil
}

// fixup 统一修正长度字段与校验和。
// fixup fixes length fields and checksums.
func (b *SerializeBuffer) fixup() {
	for _, rec := range b.layers {
		// 嵌套模型：层的段 = [start, 缓冲末尾)（内层全部包含在内，
		// 外层头部在其之前、不包含）。
		// Nested model: a layer's segment is [start, buffer end) — all inner
		// layers included, outer headers excluded.
		seg := b.buf[rec.start:]
		switch t := rec.enc.(type) {
		case *IPv4:
			if b.opts.FixLengths {
				binary.BigEndian.PutUint16(seg[2:4], uint16(len(seg)))
			}
			if b.opts.ComputeChecksums {
				seg[10], seg[11] = 0, 0
				binary.BigEndian.PutUint16(seg[10:12], InternetChecksum(seg[:t.HeaderLength()]))
			}
		case *IPv6:
			if b.opts.FixLengths {
				binary.BigEndian.PutUint16(seg[4:6], uint16(len(seg)-40))
			}
		case *UDP:
			if b.opts.FixLengths {
				binary.BigEndian.PutUint16(seg[4:6], uint16(len(seg)))
			}
			if b.opts.ComputeChecksums {
				seg[6], seg[7] = 0, 0
				binary.BigEndian.PutUint16(seg[6:8], b.transportChecksum(rec, seg, 17))
			}
		case *TCP:
			if b.opts.ComputeChecksums {
				seg[16], seg[17] = 0, 0
				binary.BigEndian.PutUint16(seg[16:18], b.transportChecksum(rec, seg, 6))
			}
		case *ICMP:
			if b.opts.ComputeChecksums {
				seg[2], seg[3] = 0, 0
				binary.BigEndian.PutUint16(seg[2:4], InternetChecksum(seg))
			}
		}
	}
}

// transportChecksum 计算 TCP/UDP 伪头校验和（网络层为 IPv4 时）。
// transportChecksum computes the TCP/UDP pseudo-header checksum for IPv4.
func (b *SerializeBuffer) transportChecksum(rec layerRec, seg []byte, proto uint8) uint16 {
	for i := len(b.layers) - 1; i >= 0; i-- {
		nl := b.layers[i]
		switch ip := nl.enc.(type) {
		case *IPv4:
			pseudo := make([]byte, 12)
			copy(pseudo[0:4], ip.SrcIP.To4())
			copy(pseudo[4:8], ip.DstIP.To4())
			pseudo[9] = proto
			binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(seg)))
			return InternetChecksum(append(pseudo, seg...))
		case *IPv6:
			pseudo := make([]byte, 40)
			copy(pseudo[0:16], ip.SrcIP.To16())
			copy(pseudo[16:32], ip.DstIP.To16())
			binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(seg)))
			pseudo[39] = proto
			return InternetChecksum(append(pseudo, seg...))
		}
	}
	return 0
}

// InternetChecksum 计算 16 位反码和（构造与校验共用工具）。
// InternetChecksum computes the 16-bit one's complement sum (shared crafting
// and validation utility).
func InternetChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i:]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

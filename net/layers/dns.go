package layers

import (
	"encoding/binary"
	"fmt"
)

// DNS 记录类型与类别（常用子集）。
// DNS record types and classes (common subset).
const (
	DNSTypeA     uint16 = 1
	DNSTypeNS    uint16 = 2
	DNSTypeCNAME uint16 = 5
	DNSTypeAAAA  uint16 = 28

	DNSClassIN uint16 = 1
)

// DNSQuestion 是 DNS 查询问题段。Name 是解引用后的域名标签序列
// （含终止 0；压缩指针已展开，重序列化不保留原指针编码）。
// DNSQuestion is a DNS query question. Name is the dereferenced label
// sequence (zero-terminated; compression pointers are expanded, so
// reserialization does not preserve the original pointer encoding).
type DNSQuestion struct {
	Name  []byte
	Type  uint16
	Class uint16
}

// DNS 是域名系统层（查询与响应头 + 问题段；答案段保留原始字节）。
// DNS is the Domain Name System layer (header + questions; answer sections
// kept raw).
type DNS struct {
	BaseLayer
	ID        uint16
	Flags     uint16
	Questions []DNSQuestion
	// AnswerBytes 是问题段之后的原始字节（答案/授权/附加段）。
	AnswerBytes []byte
}

// LayerType 返回 LayerTypeDNS。
// LayerType returns LayerTypeDNS.
func (d *DNS) LayerType() LayerType { return LayerTypeDNS }

// String 描述 DNS 报文。
// String describes the DNS message.
func (d *DNS) String() string {
	return fmt.Sprintf("DNS id=%d flags=%#04x questions=%d", d.ID, d.Flags, len(d.Questions))
}

// QDCount 返回问题段数量。
// QDCount returns the question count.
func (d *DNS) QDCount() uint16 { return uint16(len(d.Questions)) }

type dnsDecoder struct{}

// Decode 解析 DNS 头与问题段（支持压缩指针）；答案段原样保留。
// Decode parses the DNS header and questions (compression pointers
// supported); answer sections are kept raw.
func (dnsDecoder) Decode(data []byte) (Layer, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("%w: dns message too short: %d", ErrTruncated, len(data))
	}
	d := &DNS{
		BaseLayer: BaseLayer{Contents: append([]byte(nil), data[:12]...)},
		ID:        binary.BigEndian.Uint16(data[0:2]),
		Flags:     binary.BigEndian.Uint16(data[2:4]),
	}
	qd := int(binary.BigEndian.Uint16(data[4:6]))
	off := 12
	for i := 0; i < qd; i++ {
		name, next, err := parseDNSName(data, off)
		if err != nil {
			return nil, err
		}
		off = next
		if len(data) < off+4 {
			return nil, fmt.Errorf("layers: dns question truncated")
		}
		d.Questions = append(d.Questions, DNSQuestion{
			Name:  name,
			Type:  binary.BigEndian.Uint16(data[off : off+2]),
			Class: binary.BigEndian.Uint16(data[off+2 : off+4]),
		})
		off += 4
	}
	if off < len(data) {
		d.AnswerBytes = append([]byte(nil), data[off:]...)
	}
	return d, nil
}

// parseDNSName 解析域名（支持压缩指针），返回原始标签序列与下一偏移。
// next 记录域名在报文中的结束位置：仅在遇到第一个压缩指针时定格，
// 普通标签路径随 off 前进，终止标签后为 off+1。
//
// parseDNSName parses a domain name (compression pointers supported),
// returning the raw label sequence and the next offset. next is the name's
// end position in the message: fixed when the first compression pointer is
// seen; for plain labels it follows off, ending at off+1 after the
// terminator.
func parseDNSName(data []byte, off int) ([]byte, int, error) {
	var name []byte
	end := off
	ptrSeen := false
	for hops := 0; hops < 128; hops++ {
		if off >= len(data) {
			return nil, 0, fmt.Errorf("%w: dns name truncated", ErrTruncated)
		}
		l := int(data[off])
		switch {
		case l == 0:
			name = append(name, 0)
			if !ptrSeen {
				end = off + 1
			}
			return name, end, nil
		case l&0xC0 == 0xC0:
			if len(data) < off+2 {
				return nil, 0, fmt.Errorf("%w: dns name pointer truncated", ErrTruncated)
			}
			if !ptrSeen {
				end = off + 2
				ptrSeen = true
			}
			ptr := int(binary.BigEndian.Uint16(data[off:off+2]) & 0x3FFF)
			if ptr >= off {
				return nil, 0, fmt.Errorf("layers: dns name pointer loop")
			}
			off = ptr
			continue
		case l&0xC0 != 0:
			return nil, 0, fmt.Errorf("layers: dns unsupported label type %#x", l)
		}
		if len(data) < off+1+l {
			return nil, 0, fmt.Errorf("%w: dns label truncated", ErrTruncated)
		}
		name = append(name, data[off:off+1+l]...)
		off += 1 + l
	}
	return nil, 0, fmt.Errorf("layers: dns name too deep")
}

// SerializeTo 序列化 DNS 头与问题段（答案段原样附后）。
// SerializeTo serializes the DNS header and questions (answer bytes
// appended verbatim).
func (d *DNS) SerializeTo(b *SerializeBuffer) error {
	size := 12
	for _, q := range d.Questions {
		size += len(q.Name) + 4
	}
	size += len(d.AnswerBytes)
	buf := b.PrependHeader(size, d)
	binary.BigEndian.PutUint16(buf[0:2], d.ID)
	binary.BigEndian.PutUint16(buf[2:4], d.Flags)
	binary.BigEndian.PutUint16(buf[4:6], uint16(len(d.Questions)))
	off := 12
	for _, q := range d.Questions {
		copy(buf[off:], q.Name)
		off += len(q.Name)
		binary.BigEndian.PutUint16(buf[off:off+2], q.Type)
		binary.BigEndian.PutUint16(buf[off+2:off+4], q.Class)
		off += 4
	}
	copy(buf[off:], d.AnswerBytes)
	return nil
}

// DNSName 把点分域名编码为 DNS 标签序列（含结尾 0）。
// DNSName encodes a dotted domain into DNS label sequence (zero-terminated).
func DNSName(domain string) []byte {
	var out []byte
	start := 0
	for i := 0; i <= len(domain); i++ {
		if i == len(domain) || domain[i] == '.' {
			if i > start {
				out = append(out, byte(i-start))
				out = append(out, domain[start:i]...)
			}
			start = i + 1
		}
	}
	return append(out, 0)
}

// DNSNameToString 把无压缩指针的标签序列还原为点分名。
// DNSNameToString decodes a pointer-free label sequence back to dotted form.
func DNSNameToString(name []byte) (string, error) {
	var out []byte
	for off := 0; off < len(name); {
		l := int(name[off])
		if l == 0 {
			break
		}
		if l&0xC0 != 0 {
			return "", fmt.Errorf("layers: dns name has compression pointer")
		}
		if off+1+l > len(name) {
			return "", fmt.Errorf("layers: dns name label truncated")
		}
		if len(out) > 0 {
			out = append(out, '.')
		}
		out = append(out, name[off+1:off+1+l]...)
		off += 1 + l
	}
	return string(out), nil
}

func init() {
	RegisterLayerDecoder(LayerTypeDNS, dnsDecoder{})
}

package replay

import (
	"encoding/binary"
	"fmt"
	"net"
)

// rewriter 重写报文的源/目的 IP 与 MAC，并用 RFC 1624 增量更新校验和。
// rewriter rewrites src/dst IP and MAC, updating checksums incrementally per
// RFC 1624.
type rewriter struct {
	srcIP, dstIP   [4]byte
	srcMAC, dstMAC [6]byte
}

// newRewriter 解析重写参数。
// newRewriter parses the rewrite parameters.
func newRewriter(c RewriteConfig) (*rewriter, error) {
	r := &rewriter{}
	if c.SrcIP != "" {
		ip := net.ParseIP(c.SrcIP).To4()
		if ip == nil {
			return nil, fmt.Errorf("replay: invalid src IP %q", c.SrcIP)
		}
		copy(r.srcIP[:], ip)
	}
	if c.DstIP != "" {
		ip := net.ParseIP(c.DstIP).To4()
		if ip == nil {
			return nil, fmt.Errorf("replay: invalid dst IP %q", c.DstIP)
		}
		copy(r.dstIP[:], ip)
	}
	if c.SrcMAC != "" {
		mac, err := net.ParseMAC(c.SrcMAC)
		if err != nil || len(mac) != 6 {
			return nil, fmt.Errorf("replay: invalid src MAC %q", c.SrcMAC)
		}
		copy(r.srcMAC[:], mac)
	}
	if c.DstMAC != "" {
		mac, err := net.ParseMAC(c.DstMAC)
		if err != nil || len(mac) != 6 {
			return nil, fmt.Errorf("replay: invalid dst MAC %q", c.DstMAC)
		}
		copy(r.dstMAC[:], mac)
	}
	return r, nil
}

// rewrite 返回重写后的帧副本。
// rewrite returns the rewritten frame copy.
func (r *rewriter) rewrite(frame []byte) ([]byte, error) {
	if len(frame) < 14 {
		return nil, fmt.Errorf("replay: frame too short: %d", len(frame))
	}
	out := append([]byte(nil), frame...)
	etherType := binary.BigEndian.Uint16(out[12:14])
	// MAC 重写（以太网头 0-6 dst，6-12 src）。
	if r.dstMAC != [6]byte{} {
		copy(out[0:6], r.dstMAC[:])
	}
	if r.srcMAC != [6]byte{} {
		copy(out[6:12], r.srcMAC[:])
	}
	if etherType == 0x0800 && len(out) >= 34 { // IPv4
		if err := rewriteIPv4Checksums(out[14:], r.srcIP, r.dstIP); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// rewriteIPv4Checksums 重写 IPv4 包内的源/目的 IP 并增量更新
// IPv4 头与 TCP/UDP 伪头校验和。
//
// rewriteIPv4Checksums rewrites src/dst IP inside an IPv4 packet, updating
// the IPv4 header and TCP/UDP pseudo-header checksums incrementally.
func rewriteIPv4Checksums(ipPkt []byte, srcIP, dstIP [4]byte) error {
	if len(ipPkt) < 20 {
		return fmt.Errorf("replay: short ipv4 packet")
	}
	ihl := int(ipPkt[0]&0x0f) * 4
	if ihl < 20 || len(ipPkt) < ihl {
		return fmt.Errorf("replay: bad ipv4 header length")
	}
	proto := ipPkt[9]
	// 先抓旧字段值：增量更新（RFC 1624）依赖替换前的值。
	oldSrc := binary.BigEndian.Uint32(ipPkt[12:16])
	oldDst := binary.BigEndian.Uint32(ipPkt[16:20])
	// 增量更新 IPv4 头校验和：HC' = ~(~HC + ~m + m')。
	if srcIP != [4]byte{} {
		hc := binary.BigEndian.Uint16(ipPkt[10:12])
		binary.BigEndian.PutUint32(ipPkt[12:16], binary.BigEndian.Uint32(srcIP[:]))
		hc = incrementalChecksum(hc, oldSrc, binary.BigEndian.Uint32(srcIP[:]))
		binary.BigEndian.PutUint16(ipPkt[10:12], hc)
	}
	if dstIP != [4]byte{} {
		hc := binary.BigEndian.Uint16(ipPkt[10:12])
		binary.BigEndian.PutUint32(ipPkt[16:20], binary.BigEndian.Uint32(dstIP[:]))
		hc = incrementalChecksum(hc, oldDst, binary.BigEndian.Uint32(dstIP[:]))
		binary.BigEndian.PutUint16(ipPkt[10:12], hc)
	}
	// 传输层伪头校验和。
	switch proto {
	case 6, 17: // TCP/UDP
		if len(ipPkt) < ihl+8 {
			return nil // 无完整传输头：跳过
		}
		off := ihl + 16 // 校验和字段在传输头内的偏移
		tc := binary.BigEndian.Uint16(ipPkt[off : off+2])
		if srcIP != [4]byte{} {
			tc = incrementalChecksum(tc, oldSrc, binary.BigEndian.Uint32(srcIP[:]))
		}
		if dstIP != [4]byte{} {
			tc = incrementalChecksum(tc, oldDst, binary.BigEndian.Uint32(dstIP[:]))
		}
		binary.BigEndian.PutUint16(ipPkt[off:off+2], tc)
	}
	return nil
}

// incrementalChecksum 按 RFC 1624 做单字段替换的增量校验和更新。
// incrementalChecksum updates a checksum for one replaced 32-bit field per
// RFC 1624.
func incrementalChecksum(oldSum uint16, oldField, newField uint32) uint16 {
	// HC' = ~(~HC + ~m + m')
	sum := uint32(^oldSum)
	sum += ^oldField & 0xffff
	sum += ^oldField >> 16
	sum += newField & 0xffff
	sum += newField >> 16
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

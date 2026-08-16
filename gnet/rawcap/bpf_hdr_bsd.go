//go:build freebsd || netbsd || openbsd || dragonfly

package rawcap

import (
	"encoding/binary"
	"time"
)

// 非 darwin BSD 的 bpf_hdr 用 timeval（64 位 long，16 字节时间戳）。
// Non-darwin BSD bpf_hdr uses timeval (64-bit long, 16-byte timestamp).
const bpfHdrLenExtra = 8

// parseBPFHdr 解析 BSD 的 BPF 头。
// parseBPFHdr parses the BSD BPF header.
func parseBPFHdr(buf []byte) (time.Time, int, int, int) {
	sec := int64(binary.LittleEndian.Uint64(buf[0:8]))
	usec := int64(binary.LittleEndian.Uint64(buf[8:16]))
	caplen := int(binary.LittleEndian.Uint32(buf[16:20]))
	datalen := int(binary.LittleEndian.Uint32(buf[20:24]))
	hdrlen := int(binary.LittleEndian.Uint16(buf[24:26]))
	return time.Unix(sec, usec*1000), caplen, datalen, hdrlen
}

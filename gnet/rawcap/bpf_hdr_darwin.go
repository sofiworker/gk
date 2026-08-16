//go:build darwin

package rawcap

import (
	"encoding/binary"
	"time"
)

// darwin 的 bpf_hdr 用 timeval32（8 字节时间戳）。
// darwin's bpf_hdr uses timeval32 (8-byte timestamp).
const bpfHdrLenExtra = 0

// parseBPFHdr 解析 darwin 的 BPF 头。
// parseBPFHdr parses the darwin BPF header.
func parseBPFHdr(buf []byte) (time.Time, int, int, int) {
	sec := int64(binary.LittleEndian.Uint32(buf[0:4]))
	usec := int64(binary.LittleEndian.Uint32(buf[4:8]))
	caplen := int(binary.LittleEndian.Uint32(buf[8:12]))
	datalen := int(binary.LittleEndian.Uint32(buf[12:16]))
	hdrlen := int(binary.LittleEndian.Uint16(buf[16:18]))
	return time.Unix(sec, usec*1000), caplen, datalen, hdrlen
}

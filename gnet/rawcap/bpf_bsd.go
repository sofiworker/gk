//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package rawcap

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// 本文件是 macOS/BSD 的 /dev/bpf 捕获后端（纯 Go：open + ioctl + read，
// 无 cgo）。BPF 头的时间戳布局因平台而异：darwin 用 timeval32（8 字节），
// 其余 BSD 用 timeval（16 字节），见 bpfHdrLen。
//
// This file is the macOS/BSD /dev/bpf capture backend (pure Go: open +
// ioctl + read, no cgo). The BPF header timestamp layout differs by
// platform: darwin uses timeval32 (8 bytes), other BSDs use timeval (16
// bytes); see bpfHdrLen.

// BPF ioctl 常量（4.4BSD 起源，各 BSD 一致）。
// BPF ioctl constants (4.4BSD lineage, consistent across the BSDs).
const (
	biocGBLEN     = 0x40044266 // 读缓冲长度
	biocSETIF     = 0x80204267 // 绑定网卡
	biocPROMISC   = 0x20004269 // 混杂模式
	biocIMMEDIATE = 0x80044270 // 立即模式：有数据即返回
	biocSDLT      = 0x80044278 // 链路层类型（可选）
	biocSBLEN     = 0x80044266 // 设置缓冲长度
	biocSHDRCMPLT = 0x80044275 // 完整头（macOS 用 BIOCSHDRCMPLT）
)

// bpfHdrLen 是平台 BPF 头长度（darwin timeval32=8，其余 timeval=16）。
// bpfHdrLen is the platform BPF header length (darwin timeval32=8, others
// timeval=16).
const bpfHdrLen = 8 // +8 on !darwin via bpfHdrLenExtra

// bpfHandle 是 /dev/bpf 捕获句柄。
// bpfHandle is a /dev/bpf capture handle.
type bpfHandle struct {
	fd     int
	iface  string
	stats  Stats
	buf    []byte
	closed bool
}

// openLive 打开并绑定 /dev/bpf 设备。
// openLive opens and binds a /dev/bpf device.
func openLive(interfaceName string, cfg Config) (Handle, error) {
	fd := -1
	// 遍历 bpf0..bpf31 找可用设备。
	for i := 0; i < 32; i++ {
		path := fmt.Sprintf("/dev/bpf%d", i)
		f, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err == nil {
			fd = f
			break
		}
		if !errors.Is(err, syscall.EBUSY) && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOENT) {
			return nil, fmt.Errorf("rawcap: open %s: %w", path, err)
		}
	}
	if fd < 0 {
		return nil, fmt.Errorf("rawcap: no free /dev/bpf device")
	}
	h := &bpfHandle{fd: fd, iface: interfaceName, buf: make([]byte, cfg.SnapLen+bpfHdrLen+bpfHdrLenExtra)}

	// 绑定网卡。
	var ifr [32]byte
	copy(ifr[:], interfaceName)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), biocSETIF, uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("rawcap: BIOCSETIF %s: %w", interfaceName, errno)
	}
	// 立即模式：有包即返回。
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), biocIMMEDIATE, 1); errno != 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("rawcap: BIOCIMMEDIATE: %w", errno)
	}
	// 混杂模式。
	if cfg.Promiscuous {
		_, _, _ = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), biocPROMISC, 1)
	}
	// 缓冲长度对齐 snaplen。
	if cfg.SnapLen > 0 {
		_, _, _ = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), biocSBLEN, uintptr(cfg.SnapLen))
	}
	return h, nil
}

// ReadPacket 读取下一个包；解析 BPF 头。
// ReadPacket reads the next packet, parsing the BPF header.
func (h *bpfHandle) ReadPacket() (*Packet, error) {
	if h.closed {
		return nil, io.ErrClosedPipe
	}
	n, err := unix.Read(h.fd, h.buf)
	if err != nil {
		return nil, err
	}
	if n < bpfHdrLen+bpfHdrLenExtra {
		return nil, fmt.Errorf("rawcap: short bpf read: %d", n)
	}
	ts, caplen, datalen, hdrlen := parseBPFHdr(h.buf[:n])
	if hdrlen > n || caplen > n-hdrlen {
		return nil, fmt.Errorf("rawcap: malformed bpf header")
	}
	data := append([]byte(nil), h.buf[hdrlen:hdrlen+caplen]...)
	h.stats.PacketsReceived++
	return &Packet{
		Data: data,
		Info: &PacketInfo{Timestamp: ts, CaptureLength: caplen, Length: datalen},
	}, nil
}

// WritePacketData 注入报文（/dev/bpf 写即发送）。
// WritePacketData injects a packet (writing to /dev/bpf sends).
func (h *bpfHandle) WritePacketData(pkt []byte) error {
	if h.closed {
		return io.ErrClosedPipe
	}
	_, err := unix.Write(h.fd, pkt)
	return err
}

// RawHandle 返回底层 fd。
// RawHandle returns the underlying fd.
func (h *bpfHandle) RawHandle() (interface{}, error) {
	return h.fd, nil
}

// Stats 返回接收统计。
// Stats returns receive statistics.
func (h *bpfHandle) Stats() *Stats { return &h.stats }

// Close 关闭设备。
// Close closes the device.
func (h *bpfHandle) Close() error {
	if h.closed {
		return nil
	}
	h.closed = true
	return unix.Close(h.fd)
}

//go:build windows

package rawcap

import (
	"fmt"
	"io"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// 本文件是 Windows 捕获后端的 npcap 动态加载实现（purego：NewLazyDLL，
// 无 cgo）。防 DLL 劫持：优先按全路径加载 System32\Npcap\wpcap.dll。
// 注意：本后端未经 Windows 实测，仅交叉编译验证（见 README 平台矩阵）。
//
// This file is the Windows capture backend via npcap dynamic loading
// (purego: NewLazyDLL, no cgo). Anti-DLL-hijacking: load
// System32\Npcap\wpcap.dll by full path first. Note: this backend is not
// tested on Windows — cross-compilation verified only (see the README
// platform matrix).

// pcapPkthdr 对应 pcap_pkthdr（Windows 上 timeval 为 32 位：16 字节）。
// pcapPkthdr mirrors pcap_pkthdr (Windows timeval is 32-bit: 16 bytes).
type pcapPkthdr struct {
	Sec, Usec int32
	Caplen    uint32
	Len       uint32
}

var (
	wpcapOnce sync.Once
	wpcapErr  error
	wpcapDLL  *syscall.LazyDLL
	procOpen  *syscall.LazyProc
	procNext  *syscall.LazyProc
	procClose *syscall.LazyProc
)

// loadWpcap 加载 wpcap.dll 并解析入口。
// loadWpcap loads wpcap.dll and resolves the entry points.
func loadWpcap() error {
	wpcapOnce.Do(func() {
		for _, path := range []string{
			`C:\Windows\System32\Npcap\wpcap.dll`, // Npcap 默认全路径（防劫持）
			`wpcap.dll`,                           // 兜底：PATH 搜索
		} {
			d := syscall.NewLazyDLL(path)
			if err := d.Load(); err == nil {
				wpcapDLL = d
				break
			}
		}
		if wpcapDLL == nil {
			wpcapErr = fmt.Errorf("rawcap: wpcap.dll not found (install Npcap)")
			return
		}
		procOpen = wpcapDLL.NewProc("pcap_open_live")
		procNext = wpcapDLL.NewProc("pcap_next_ex")
		procClose = wpcapDLL.NewProc("pcap_close")
		for name, p := range map[string]*syscall.LazyProc{
			"pcap_open_live": procOpen, "pcap_next_ex": procNext, "pcap_close": procClose,
		} {
			if err := p.Find(); err != nil {
				wpcapErr = fmt.Errorf("rawcap: resolve %s: %w", name, err)
				return
			}
		}
	})
	return wpcapErr
}

// wpcapHandle 是 npcap 捕获句柄。
// wpcapHandle is the npcap capture handle.
type wpcapHandle struct {
	pcap   uintptr
	stats  Stats
	closed bool
}

// openLive 打开网卡并进入混杂/非混杂捕获。
// openLive opens an interface for promiscuous or non-promiscuous capture.
func openLive(interfaceName string, cfg Config) (Handle, error) {
	if err := loadWpcap(); err != nil {
		return nil, err
	}
	name, err := syscall.BytePtrFromString(interfaceName)
	if err != nil {
		return nil, err
	}
	promisc := int32(0)
	if cfg.Promiscuous {
		promisc = 1
	}
	snaplen := cfg.SnapLen
	if snaplen <= 0 {
		snaplen = DefaultSnapLen
	}
	timeout := int32(cfg.Timeout / time.Millisecond)
	// int pcap_open_live(const char*, int snaplen, int promisc, int to_ms,
	// char *errbuf)。
	ret, _, _ := procOpen.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(snaplen),
		uintptr(promisc),
		uintptr(timeout),
		0, // errbuf 省略：失败时 ret==0
	)
	if ret == 0 {
		return nil, fmt.Errorf("rawcap: pcap_open_live(%s) failed (no Npcap or no permission)", interfaceName)
	}
	return &wpcapHandle{pcap: ret}, nil
}

// ReadPacket 读取下一个包（pcap_next_ex）。
// ReadPacket reads the next packet (pcap_next_ex).
func (h *wpcapHandle) ReadPacket() (*Packet, error) {
	if h.closed {
		return nil, io.ErrClosedPipe
	}
	var hdr *pcapPkthdr
	var data *byte
	// int pcap_next_ex(pcap_t*, struct pcap_pkthdr**, const u_char**)。
	ret, _, _ := procNext.Call(
		h.pcap,
		uintptr(unsafe.Pointer(&hdr)),
		uintptr(unsafe.Pointer(&data)),
	)
	switch ret {
	case 1:
		buf := unsafe.Slice(data, int(hdr.Caplen))
		out := append([]byte(nil), buf...)
		h.stats.PacketsReceived++
		return &Packet{
			Data: out,
			Info: &PacketInfo{
				Timestamp:     time.Unix(int64(hdr.Sec), int64(hdr.Usec)*1000),
				CaptureLength: int(hdr.Caplen),
				Length:        int(hdr.Len),
			},
		}, nil
	case 0: // 超时
		return nil, fmt.Errorf("rawcap: pcap_next_ex timeout")
	case ^uintptr(1): // -2 的 uintptr 表示（EOF）
		return nil, io.EOF
	default:
		return nil, fmt.Errorf("rawcap: pcap_next_ex error %d", int32(ret))
	}
}

// WritePacketData 注入报文（pcap_sendpacket 未实现：返回不支持）。
// WritePacketData injects a packet (pcap_sendpacket not bound: unsupported).
func (h *wpcapHandle) WritePacketData([]byte) error {
	return fmt.Errorf("rawcap: packet injection not implemented on windows")
}

// RawHandle 返回 pcap_t 指针。
// RawHandle returns the pcap_t pointer.
func (h *wpcapHandle) RawHandle() (interface{}, error) {
	return h.pcap, nil
}

// Stats 返回接收统计。
// Stats returns receive statistics.
func (h *wpcapHandle) Stats() *Stats { return &h.stats }

// Close 关闭捕获。
// Close closes the capture.
func (h *wpcapHandle) Close() error {
	if h.closed {
		return nil
	}
	h.closed = true
	procClose.Call(h.pcap)
	return nil
}

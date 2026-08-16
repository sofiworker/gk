//go:build linux

package rawcap

import (
	"fmt"
	"net"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type LinuxHandle struct {
	fd      int
	iface   *net.Interface
	cfg     Config
	stats   Stats
	addr    unix.SockaddrLinklayer
	mu      sync.Mutex
	closed  bool
	recvBuf []byte

	ring *tpacketRing
}

type tpacketRing struct {
	data      []byte
	blockSize int
	blockNum  int
	frameSize int

	blockIdx    int
	pktOffset   uint32
	pktCount    uint32
	blockHeader *unix.TpacketHdrV1
}

func openLive(interfaceName string, cfg Config) (Handle, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("rawcap: interface %s not found: %w", interfaceName, err)
	}

	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(hostToNetwork16(unix.ETH_P_ALL)))
	if err != nil {
		return nil, fmt.Errorf("rawcap: socket: %w", err)
	}

	handle := &LinuxHandle{
		fd:    fd,
		iface: iface,
		cfg:   cfg,
		addr: unix.SockaddrLinklayer{
			Protocol: hostToNetwork16(unix.ETH_P_ALL),
			Ifindex:  iface.Index,
		},
		recvBuf: make([]byte, cfg.SnapLen),
	}

	if err := handle.configure(); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return handle, nil
}

func (h *LinuxHandle) configure() error {
	if err := unix.Bind(h.fd, &h.addr); err != nil {
		return fmt.Errorf("rawcap: bind: %w", err)
	}

	if h.cfg.TPacketV3 {
		if err := h.enableTPacketV3(); err != nil {
			return err
		}
	}

	if h.cfg.BufferSize > 0 {
		if err := unix.SetsockoptInt(h.fd, unix.SOL_SOCKET, unix.SO_RCVBUF, h.cfg.BufferSize); err != nil {
			return fmt.Errorf("rawcap: set buffer size: %w", err)
		}
	}

	if h.cfg.Timeout > 0 {
		tv := unix.NsecToTimeval(h.cfg.Timeout.Nanoseconds())
		if err := unix.SetsockoptTimeval(h.fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
			return fmt.Errorf("rawcap: set timeout: %w", err)
		}
	}

	if h.cfg.Promiscuous {
		mreq := &unix.PacketMreq{
			Ifindex: int32(h.iface.Index),
			Type:    unix.PACKET_MR_PROMISC,
		}
		if err := unix.SetsockoptPacketMreq(h.fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, mreq); err != nil {
			return fmt.Errorf("rawcap: set promiscuous: %w", err)
		}
	}

	return nil
}

func (h *LinuxHandle) ReadPacket() (*Packet, error) {
	if h.isClosed() {
		return nil, ErrHandleClosed
	}

	if h.ring != nil {
		return h.readTPacket()
	}

	for {
		n, from, err := unix.Recvfrom(h.fd, h.recvBuf, 0)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				return nil, fmt.Errorf("%w: %v", ErrReadTimeout, err)
			}
			if err == unix.EBADF {
				return nil, ErrHandleClosed
			}
			return nil, fmt.Errorf("rawcap: recvfrom: %w", err)
		}

		if n <= 0 {
			continue
		}

		data := make([]byte, n)
		copy(data, h.recvBuf[:n])

		info := &PacketInfo{
			Timestamp:      time.Now().UTC(),
			CaptureLength:  n,
			Length:         n,
			InterfaceIndex: h.iface.Index,
		}

		if ll, ok := from.(*unix.SockaddrLinklayer); ok && ll != nil && ll.Ifindex != 0 {
			info.InterfaceIndex = ll.Ifindex
		}

		h.mu.Lock()
		h.stats.PacketsReceived++
		h.mu.Unlock()

		return &Packet{
			Data: data,
			Info: info,
		}, nil
	}
}

func (h *LinuxHandle) WritePacketData(data []byte) error {
	if h.isClosed() {
		return ErrHandleClosed
	}
	_, err := unix.Write(h.fd, data)
	if err == unix.EBADF {
		return ErrHandleClosed
	}
	if err != nil {
		return fmt.Errorf("rawcap: write packet: %w", err)
	}
	return nil
}

func (h *LinuxHandle) RawHandle() (interface{}, error) {
	if h.isClosed() {
		return nil, ErrHandleClosed
	}
	return h.fd, nil
}

func (h *LinuxHandle) Stats() *Stats {
	h.mu.Lock()
	stats := h.stats
	h.mu.Unlock()
	if tp, err := unix.GetsockoptTpacketStats(h.fd, unix.SOL_PACKET, unix.PACKET_STATISTICS); err == nil {
		stats.PacketsReceived = uint64(tp.Packets)
		stats.PacketsDropped = uint64(tp.Drops)
	}
	return &stats
}

func (h *LinuxHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}
	h.closed = true

	if h.cfg.Promiscuous {
		mreq := &unix.PacketMreq{
			Ifindex: int32(h.iface.Index),
			Type:    unix.PACKET_MR_PROMISC,
		}
		_ = unix.SetsockoptPacketMreq(h.fd, unix.SOL_PACKET, unix.PACKET_DROP_MEMBERSHIP, mreq)
	}

	if h.ring != nil {
		_ = unix.Munmap(h.ring.data)
	}

	return unix.Close(h.fd)
}

func (h *LinuxHandle) isClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

func hostToNetwork16(i uint16) uint16 {
	return (i<<8)&0xff00 | i>>8
}

func (h *LinuxHandle) enableTPacketV3() error {
	if err := unix.SetsockoptInt(h.fd, unix.SOL_PACKET, unix.PACKET_VERSION, unix.TPACKET_V3); err != nil {
		return fmt.Errorf("rawcap: set tpacket v3: %w", err)
	}

	req := &unix.TpacketReq3{
		Block_size:       uint32(h.cfg.BlockSize),
		Block_nr:         uint32(h.cfg.NumBlocks),
		Frame_size:       uint32(h.cfg.FrameSize),
		Frame_nr:         uint32(h.cfg.BlockSize/h.cfg.FrameSize) * uint32(h.cfg.NumBlocks),
		Retire_blk_tov:   64, // ms
		Feature_req_word: 0,
	}
	if err := unix.SetsockoptTpacketReq3(h.fd, unix.SOL_PACKET, unix.PACKET_RX_RING, req); err != nil {
		return fmt.Errorf("rawcap: set tpacket req3: %w", err)
	}

	data, err := unix.Mmap(h.fd, 0, int(req.Block_size*req.Block_nr), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("rawcap: mmap ring: %w", err)
	}

	h.ring = &tpacketRing{
		data:      data,
		blockSize: int(req.Block_size),
		blockNum:  int(req.Block_nr),
		frameSize: int(req.Frame_size),
		blockIdx:  0,
	}
	return nil
}

func (h *LinuxHandle) readTPacket() (*Packet, error) {
	r := h.ring
	for {
		if h.isClosed() {
			return nil, ErrHandleClosed
		}
		// 确保存在包含数据的 block；ensure the block has data.
		if r.blockHeader == nil || r.pktOffset == 0 || r.pktOffset >= uint32(r.blockSize) {
			if err := h.nextBlock(); err != nil {
				if err == unix.EAGAIN {
					// 无数据：poll 等待（尊重 cfg.Timeout，0 则阻塞），
					// 避免忙等。
					if err := h.waitRingReadable(); err != nil {
						return nil, err
					}
					continue
				}
				return nil, err
			}
		}

		// 包数据提取与 Close 的 munmap 互斥：持锁读取并复制。
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, ErrHandleClosed
		}
		// block 内的包指针；packet pointer within the block.
		blockStart := r.blockIdx * r.blockSize
		pktHdr := (*unix.Tpacket3Hdr)(unsafe.Add(unsafe.Pointer(&r.data[blockStart]), uintptr(r.pktOffset)))

		if pktHdr.Status&unix.TP_STATUS_USER == 0 {
			// 不应发生，重试；should not happen, retry.
			h.mu.Unlock()
			r.blockHeader = nil
			continue
		}

		// tpacket3_hdr.Mac 是包数据相对 block 起点的偏移，
		// 包实际位置 = blockStart + pktOffset + Mac。
		start := blockStart + int(r.pktOffset) + int(pktHdr.Mac)
		end := start + int(pktHdr.Snaplen)
		// 损坏 snaplen 不得跨出当前 block。
		if end > blockStart+r.blockSize || end > len(r.data) {
			// 数据损坏，丢弃该 block；corrupted, drop the block.
			r.blockHeader.Block_status = unix.TP_STATUS_KERNEL
			r.blockHeader = nil
			h.mu.Unlock()
			continue
		}
		payload := make([]byte, pktHdr.Snaplen)
		copy(payload, r.data[start:end])
		h.mu.Unlock()

		ts := time.Unix(int64(pktHdr.Sec), int64(pktHdr.Nsec)).UTC()
		info := &PacketInfo{
			Timestamp:      ts,
			CaptureLength:  int(pktHdr.Snaplen),
			Length:         int(pktHdr.Len),
			InterfaceIndex: h.iface.Index,
		}
		h.mu.Lock()
		h.stats.PacketsReceived++
		h.mu.Unlock()

		// 移动到 block 中的下一个包；move to the next packet in the block.
		if pktHdr.Next_offset == 0 {
			r.pktOffset = uint32(r.blockSize)
		} else {
			r.pktOffset += pktHdr.Next_offset
		}
		r.pktCount++
		if r.pktCount >= r.blockHeader.Num_pkts || r.pktOffset >= uint32(r.blockSize) {
			r.blockHeader.Block_status = unix.TP_STATUS_KERNEL
			r.blockHeader = nil
			r.pktOffset = 0
			r.pktCount = 0
			r.blockIdx = (r.blockIdx + 1) % r.blockNum
		}

		return &Packet{
			Data: payload,
			Info: info,
		}, nil
	}
}

// waitRingReadable 等待 TPACKET 环形有数据；超时返回 ErrReadTimeout。
// waitRingReadable waits for TPACKET ring data; returns ErrReadTimeout on
// timeout.
func (h *LinuxHandle) waitRingReadable() error {
	var timeout int
	if h.cfg.Timeout > 0 {
		timeout = int(h.cfg.Timeout / time.Millisecond)
		if timeout < 1 {
			timeout = 1
		}
	} else {
		timeout = -1 // 无限
	}
	pfd := []unix.PollFd{{Fd: int32(h.fd), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(pfd, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return fmt.Errorf("rawcap: poll ring: %w", err)
		}
		if h.isClosed() {
			return ErrHandleClosed
		}
		if timeout > 0 && pfd[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) == 0 {
			return fmt.Errorf("%w: tpacket", ErrReadTimeout)
		}
		return nil
	}
}

func (h *LinuxHandle) nextBlock() error {
	r := h.ring
	for i := 0; i < r.blockNum; i++ {
		blockStart := r.blockIdx * r.blockSize
		desc := (*unix.TpacketBlockDesc)(unsafe.Pointer(&r.data[blockStart]))
		hdr := (*unix.TpacketHdrV1)(unsafe.Pointer(&desc.Hdr[0]))
		if hdr.Block_status&unix.TP_STATUS_USER == 0 {
			r.blockIdx = (r.blockIdx + 1) % r.blockNum
			continue
		}
		r.blockHeader = hdr
		r.pktOffset = hdr.Offset_to_first_pkt
		r.pktCount = 0
		return nil
	}
	return unix.EAGAIN
}

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
				return nil, fmt.Errorf("rawcap: read timeout: %w", err)
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

		h.stats.PacketsReceived++

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
	return err
}

func (h *LinuxHandle) RawHandle() (interface{}, error) {
	if h.isClosed() {
		return nil, ErrHandleClosed
	}
	return h.fd, nil
}

func (h *LinuxHandle) Stats() *Stats {
	stats := h.stats
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
		// Ensure we have a block with data
		if r.blockHeader == nil || r.pktOffset == 0 || r.pktOffset >= uint32(r.blockSize) {
			if err := h.nextBlock(); err != nil {
				if err == unix.EAGAIN {
					continue
				}
				return nil, err
			}
		}

		// packet pointer within block
		blockStart := r.blockIdx * r.blockSize
		ptr := uintptr(unsafe.Pointer(&r.data[blockStart])) + uintptr(r.pktOffset)
		pktHdr := (*unix.Tpacket3Hdr)(unsafe.Pointer(ptr))

		if pktHdr.Status&unix.TP_STATUS_USER == 0 {
			// should not happen, retry
			r.blockHeader = nil
			continue
		}

		start := blockStart + int(pktHdr.Mac)
		end := start + int(pktHdr.Snaplen)
		if end > len(r.data) {
			// corrupted, drop block
			r.blockHeader.Block_status = unix.TP_STATUS_KERNEL
			r.blockHeader = nil
			continue
		}
		payload := make([]byte, pktHdr.Snaplen)
		copy(payload, r.data[start:end])

		ts := time.Unix(int64(pktHdr.Sec), int64(pktHdr.Nsec)).UTC()
		info := &PacketInfo{
			Timestamp:      ts,
			CaptureLength:  int(pktHdr.Snaplen),
			Length:         int(pktHdr.Len),
			InterfaceIndex: h.iface.Index,
		}
		h.stats.PacketsReceived++

		// Move to next packet in block
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

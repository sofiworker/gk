package netinfo

import "net"

// ConnInfo 是单个连接（来自 /proc/net/tcp{,6} 与 udp{,6}）。
// ConnInfo is one connection (from /proc/net/tcp{,6} and udp{,6}).
type ConnInfo struct {
	Proto      string // tcp / udp
	LocalIP    net.IP
	LocalPort  uint16
	RemoteIP   net.IP
	RemotePort uint16
	State      string // TCP 状态名；UDP 为空
	UID        uint32
	Inode      uint32
}

// IOCountersStat 是单网卡的收发统计（来自 /proc/net/dev）。
// IOCountersStat is one interface's rx/tx counters (from /proc/net/dev).
type IOCountersStat struct {
	Name      string
	RxBytes   uint64
	RxPackets uint64
	RxErrors  uint64
	RxDropped uint64
	TxBytes   uint64
	TxPackets uint64
	TxErrors  uint64
	TxDropped uint64
}

//go:build linux

package ethtool

import (
	"net"
	"testing"
)

// TestGetLive 实测网卡 ethtool 信息（ioctl 可用时）。
// TestGetLive exercises real ethtool info when ioctl is available.
func TestGetLive(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skip(err)
	}
	got := 0
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		info, err := Get(ifi.Name)
		if err != nil {
			continue // 虚拟网卡可能不支持 ethtool
		}
		got++
		t.Logf("%s: driver=%s speed=%d duplex=%s", ifi.Name, info.Driver, info.Speed, info.Duplex)
		if got >= 2 {
			return
		}
	}
	if got == 0 {
		t.Skip("no ethtool-capable interface found")
	}
}

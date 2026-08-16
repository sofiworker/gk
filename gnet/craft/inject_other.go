//go:build !linux

package craft

import (
	"fmt"
	"net"
)

// NewAFPacketInjector 在无 AF_PACKET 的平台返回不支持错误
// （Windows 的 npcap 注入后端规划于 M5）。
//
// NewAFPacketInjector returns an unsupported error on platforms without
// AF_PACKET (the Windows npcap injection backend is planned in M5).
func NewAFPacketInjector(iface string) (Injector, error) {
	_ = net.InterfaceByName
	return nil, fmt.Errorf("craft: packet injection not supported on this platform")
}

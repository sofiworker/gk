//go:build windows

package addr

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// GetAdaptersAddresses 使用 AF_UNSPEC；AF_UNSPEC for GetAdaptersAddresses.
	familyUnspec = windows.AF_UNSPEC
)

func list(iface string) ([]Address, error) {
	adapters, err := fetchAdapters()
	if err != nil {
		return nil, err
	}
	var out []Address
	for _, a := range adapters {
		if iface != "" && a.Name != iface && a.FriendlyName != iface {
			continue
		}
		for _, ua := range a.Unicast {
			out = append(out, Address{
				IfIndex: int(a.IfIndex),
				IfName:  a.Name,
				IPNet:   ua,
				Scope:   scopeOf(ua.IP),
			})
		}
	}
	return out, nil
}

func add(Address) error {
	return ErrNotSupported
}

func deleteAddr(Address) error {
	return ErrNotSupported
}

type adapterInfo struct {
	Name         string
	FriendlyName string
	IfIndex      uint32
	Unicast      []*net.IPNet
}

// scopeOf 按地址族给粗略作用域（IPv6 link-local=2，其余=0；Windows 侧
// 无精确 unicast scope 源，Linux 侧为 netlink scope）。
// scopeOf returns a coarse scope by address family (IPv6 link-local=2,
// otherwise 0; Windows lacks a precise unicast scope source, Linux uses the
// netlink scope).
func scopeOf(ip net.IP) int {
	if ip.To4() == nil && (ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return 2
	}
	return 0
}

func fetchAdapters() ([]adapterInfo, error) {
	// 表大小可能在两次调用间变化：重试直至容纳全部条目；size=0 直接返回空。
	flags := uint32(windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_DNS_SERVER)
	for attempt := 0; attempt < 3; attempt++ {
		var size uint32
		err := windows.GetAdaptersAddresses(familyUnspec, flags, 0, nil, &size)
		if err == windows.ERROR_NO_DATA {
			return nil, nil
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			if err != nil {
				return nil, fmt.Errorf("addr: GetAdaptersAddresses size: %w", err)
			}
		}
		if size == 0 {
			return nil, nil
		}
		buf := make([]byte, size)
		adapter := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err = windows.GetAdaptersAddresses(familyUnspec, flags, 0, adapter, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue // 表变大：重试
		}
		if err != nil {
			return nil, fmt.Errorf("addr: GetAdaptersAddresses: %w", err)
		}
		return parseAdapters(adapter), nil
	}
	return nil, fmt.Errorf("addr: adapter table keeps growing")
}

// parseAdapters 遍历 IpAdapterAddresses 链表。
// parseAdapters walks the IpAdapterAddresses linked list.
func parseAdapters(adapter *windows.IpAdapterAddresses) []adapterInfo {
	var adapters []adapterInfo
	for a := adapter; a != nil; a = a.Next {
		info := adapterInfo{
			Name:         windows.BytePtrToString(a.AdapterName),
			FriendlyName: windows.UTF16PtrToString(a.FriendlyName),
			IfIndex:      a.IfIndex,
		}
		for ua := a.FirstUnicastAddress; ua != nil; ua = ua.Next {
			if ipnet := socketAddressToIPNet(ua.Address, ua.OnLinkPrefixLength); ipnet != nil {
				info.Unicast = append(info.Unicast, ipnet)
			}
		}
		adapters = append(adapters, info)
	}
	return adapters
}

func socketAddressToIPNet(sa windows.SocketAddress, prefixLen uint8) *net.IPNet {
	if sa.Sockaddr == nil {
		return nil
	}
	rsa := (*windows.RawSockaddrAny)(unsafe.Pointer(sa.Sockaddr))
	switch rsa.Addr.Family {
	case windows.AF_INET:
		sa4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(sa.Sockaddr))
		ip := net.IP(sa4.Addr[:])
		mask := net.CIDRMask(int(prefixLen), 32)
		if mask == nil {
			return nil
		}
		return &net.IPNet{IP: ip, Mask: mask}
	case windows.AF_INET6:
		sa6 := (*windows.RawSockaddrInet6)(unsafe.Pointer(sa.Sockaddr))
		ip := net.IP(sa6.Addr[:])
		mask := net.CIDRMask(int(prefixLen), 128)
		if mask == nil {
			return nil
		}
		return &net.IPNet{IP: ip, Mask: mask}
	default:
		return nil
	}
}

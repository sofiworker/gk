//go:build windows

package addr

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// AF_UNSPEC for GetAdaptersAddresses
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
				Scope:   int(a.UnicastScope),
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
	UnicastScope uint32
	Unicast      []*net.IPNet
}

func fetchAdapters() ([]adapterInfo, error) {
	var size uint32
	err := windows.GetAdaptersAddresses(familyUnspec, windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST|windows.GAA_FLAG_SKIP_DNS_SERVER, 0, nil, &size)
	if err != windows.ERROR_BUFFER_OVERFLOW {
		if err != nil {
			return nil, fmt.Errorf("GetAdaptersAddresses size: %w", err)
		}
	}

	buf := make([]byte, size)
	adapter := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
	if err := windows.GetAdaptersAddresses(familyUnspec, windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST|windows.GAA_FLAG_SKIP_DNS_SERVER, 0, adapter, &size); err != nil {
		return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
	}

	var adapters []adapterInfo
	for a := adapter; a != nil; a = a.Next {
		info := adapterInfo{
			Name:         windows.ByteSliceToString(a.AdapterName[:]),
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
	return adapters, nil
}

func socketAddressToIPNet(sa windows.SocketAddress, prefixLen uint8) *net.IPNet {
	if sa.Sockaddr == nil {
		return nil
	}
	rsa := (*windows.RawSockaddrAny)(sa.Sockaddr)
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

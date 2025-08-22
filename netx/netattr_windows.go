package netx

import (
	"fmt"
	"net"
	"unsafe"
)

var (
	modiphlpapi                           = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetAdaptersAddresses              = modiphlpapi.NewProc("GetAdaptersAddresses")
	procGetIfEntry2                       = modiphlpapi.NewProc("GetIfEntry2")
	procGetAdaptersInfo                   = modiphlpapi.NewProc("GetAdaptersInfo")
	modsetupapi                           = windows.NewLazySystemDLL("setupapi.dll")
	procSetupDiGetClassDevsW              = modsetupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInfo             = modsetupapi.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiGetDeviceRegistryPropertyW = modsetupapi.NewProc("SetupDiGetDeviceRegistryPropertyW")
)

func fillPlatform(attr *NicAttr) {
	// 通过 GetAdaptersAddresses 获取描述、DHCP、DNS
	getWindowsNicDetail(attr)
	// 通过 WMI/SetupAPI 获取 PCI 信息
	getWindowsPciInfo(attr)
}

func getWindowsNicDetail(attr *NicAttr) {
	var bufSize uint32
	procGetAdaptersAddresses.Call(
		uintptr(^uint32(0)), // AF_UNSPEC
		0x00000010|0x00000040,
		0, nil, &bufSize, 0)
	if bufSize == 0 {
		return
	}
	buf := make([]byte, bufSize)
	r1, _, _ := procGetAdaptersAddresses.Call(
		uintptr(^uint32(0)),
		0x00000010|0x00000040,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)), 0)
	if r1 != 0 {
		return
	}
	aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
	for p := aa; p != nil; p = p.Next {
		if p.FriendlyName != nil {
			name := windows.UTF16PtrToString(p.FriendlyName)
			if name == attr.Name || p.AdapterName == attr.Name {
				attr.Description = windows.UTF16PtrToString(p.Description)
				if p.DhcpEnabled != 0 && p.DhcpServer.Ipv4.sin_addr != 0 {
					attr.DHCPServer = net.IP((*[4]byte)(unsafe.Pointer(&p.DhcpServer.Ipv4.sin_addr))[:]).String()
				}
				break
			}
		}
	}
}

func getWindowsPciInfo(attr *NicAttr) {
	// 通过注册表或 SetupAPI 获取 PCI 信息
	// 简化：读取注册表
	k, err := windows.UTF16PtrFromString(fmt.Sprintf(`SYSTEM\CurrentControlSet\Control\Class\{4d36e972-e325-11ce-bfc1-08002be10318}`))
	if err != nil {
		return
	}
	h, err := windows.OpenKey(windows.HKEY_LOCAL_MACHINE, k, windows.KEY_READ)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	// 枚举子键匹配 NetCfgInstanceId
	// 省略完整枚举，仅示意
}

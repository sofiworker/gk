package gnet

import "net"

type Interface struct {
	Name         string
	MTU          int
	HardwareAddr net.HardwareAddr
	Flags        net.Flags
	Index        int
	Addresses    []net.Addr
	Description  string
	MAC          string
	Speed        int64
	Up           bool
	Loopback     bool
	Virtual      bool
	IPv4Addrs    []string
	IPv6Addrs    []string
	Subnets      []string
	Gateways     []string
	DNSServers   []string
	DHCPServer   string
	VendorID     string
	DeviceID     string
	Driver       string
	Location     string
}

// NicAttr 汇总所有可获得的属性
type NicAttr struct {
}

func Interfaces() ([]Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var result []Interface
	for _, iface := range interfaces {
		result = append(result, Interface{
			Name:         iface.Name,
			MTU:          iface.MTU,
			HardwareAddr: iface.HardwareAddr,
			Flags:        iface.Flags,
			Index:        iface.Index,
			Addresses:    nil,
		})
	}
	return result, nil
}

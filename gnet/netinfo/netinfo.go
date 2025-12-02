package netinfo

import (
	"fmt"
	"net"

	"github.com/sofiworker/gk/gnet/addr"
	"github.com/sofiworker/gk/gnet/link"
	"github.com/sofiworker/gk/gnet/route"
)

// Interface 汇总网卡及其常见网络属性。
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

// Interfaces 返回当前主机的网卡信息，结合 link/addr/route/ethtool。
func Interfaces() ([]Interface, error) {
	links, err := link.List()
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}

	addrList, err := addr.List("")
	if err != nil {
		return nil, fmt.Errorf("list addrs: %w", err)
	}
	addrByIf := groupAddrByIf(addrList)

	routes, _ := route.List(0)
	routeByIf := groupRouteByIf(routes)

	var out []Interface
	for _, l := range links {
		iface := Interface{
			Name:         l.Name,
			Index:        l.Index,
			MTU:          l.MTU,
			HardwareAddr: l.HardwareAddr,
			Flags:        l.Flags,
			Description:  l.Driver,
			MAC:          l.HardwareAddr.String(),
			Speed:        l.SpeedMbps,
			Up:           l.Up,
			Loopback:     l.Flags&net.FlagLoopback != 0,
			Virtual:      l.Flags&net.FlagLoopback != 0 || l.HardwareAddr == nil,
			Driver:       l.Driver,
		}

		if l.Ethtool != nil {
			iface.Driver = l.Ethtool.Driver
			iface.Description = l.Ethtool.Driver
			iface.VendorID = l.Ethtool.BusInfo
			iface.DeviceID = l.Ethtool.DriverVersion
		}

		if addrs := addrByIf[l.Name]; len(addrs) > 0 {
			iface.Addresses, iface.IPv4Addrs, iface.IPv6Addrs, iface.Subnets = convertAddrs(addrs)
		}

		if gw := routeByIf[l.Index]; len(gw) > 0 {
			iface.Gateways = gw
		}

		out = append(out, iface)
	}
	return out, nil
}

func groupAddrByIf(addrs []addr.Address) map[string][]addr.Address {
	m := make(map[string][]addr.Address, len(addrs))
	for _, a := range addrs {
		if a.IfName == "" {
			continue
		}
		m[a.IfName] = append(m[a.IfName], a)
	}
	return m
}

func convertAddrs(addrs []addr.Address) ([]net.Addr, []string, []string, []string) {
	var (
		all  []net.Addr
		ipv4 []string
		ipv6 []string
		nets []string
	)
	for _, a := range addrs {
		if a.IPNet == nil {
			continue
		}
		all = append(all, a.IPNet)
		nets = append(nets, a.IPNet.String())
		if ip := a.IPNet.IP; ip != nil {
			if ip.To4() != nil {
				ipv4 = append(ipv4, ip.String())
			} else {
				ipv6 = append(ipv6, ip.String())
			}
		}
	}
	return all, ipv4, ipv6, nets
}

func groupRouteByIf(routes []route.Route) map[int][]string {
	m := make(map[int][]string)
	for _, r := range routes {
		if r.IfIndex == 0 || r.Gw == nil {
			continue
		}
		m[r.IfIndex] = append(m[r.IfIndex], r.Gw.String())
	}
	return m
}

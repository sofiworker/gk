//go:build linux

package addr

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

func list(iface string) ([]Address, error) {
	var links []netlink.Link
	if iface != "" {
		link, err := netlink.LinkByName(iface)
		if err != nil {
			return nil, fmt.Errorf("link by name: %w", err)
		}
		links = []netlink.Link{link}
	} else {
		all, err := netlink.LinkList()
		if err != nil {
			return nil, fmt.Errorf("link list: %w", err)
		}
		links = all
	}

	var out []Address
	for _, l := range links {
		addrs, err := netlink.AddrList(l, netlink.FAMILY_ALL)
		if err != nil {
			return nil, fmt.Errorf("addr list: %w", err)
		}
		for _, na := range addrs {
			out = append(out, fromNetlinkAddr(l.Attrs(), na))
		}
	}
	return out, nil
}

func add(a Address) error {
	link, err := linkByAddr(a)
	if err != nil {
		return err
	}
	nla := toNetlinkAddr(a)
	if err := netlink.AddrAdd(link, nla); err != nil {
		return fmt.Errorf("addr add: %w", err)
	}
	return nil
}

func deleteAddr(a Address) error {
	link, err := linkByAddr(a)
	if err != nil {
		return err
	}
	nla := toNetlinkAddr(a)
	if err := netlink.AddrDel(link, nla); err != nil {
		return fmt.Errorf("addr del: %w", err)
	}
	return nil
}

func linkByAddr(a Address) (netlink.Link, error) {
	if a.IfName != "" {
		link, err := netlink.LinkByName(a.IfName)
		if err != nil {
			return nil, fmt.Errorf("link by name: %w", err)
		}
		return link, nil
	}
	if a.IfIndex != 0 {
		link, err := netlink.LinkByIndex(a.IfIndex)
		if err != nil {
			return nil, fmt.Errorf("link by index: %w", err)
		}
		return link, nil
	}
	return nil, fmt.Errorf("addr: interface is required")
}

func fromNetlinkAddr(attrs *netlink.LinkAttrs, na netlink.Addr) Address {
	return Address{
		IfIndex: attrs.Index,
		IfName:  attrs.Name,
		IPNet:   na.IPNet,
		Peer:    na.Peer,
		Label:   na.Label,
		Scope:   na.Scope,
		Flags:   na.Flags,
	}
}

func toNetlinkAddr(a Address) *netlink.Addr {
	return &netlink.Addr{
		IPNet: a.IPNet,
		Peer:  a.Peer,
		Label: a.Label,
		Scope: a.Scope,
		Flags: a.Flags,
	}
}

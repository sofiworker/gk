//go:build linux

package link

import (
	"fmt"
	"net"

	"github.com/sofiworker/gk/gnet/ethtool"
	"github.com/vishvananda/netlink"
)

func listLinks() ([]Link, error) {
	nlLinks, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("netlink list: %w", err)
	}
	links := make([]Link, 0, len(nlLinks))
	for _, nll := range nlLinks {
		links = append(links, fromNetlink(nll))
	}
	return links, nil
}

func fromNetlink(nll netlink.Link) Link {
	attrs := nll.Attrs()
	l := Link{
		Index:           attrs.Index,
		Name:            attrs.Name,
		MTU:             attrs.MTU,
		HardwareAddr:    normalizeHardwareAddr(attrs.HardwareAddr),
		Flags:           attrs.Flags,
		OperState:       attrs.OperState.String(),
		SpeedMbps:       UnknownSpeedMbps,
		Duplex:          ethtool.DuplexUnknown,
		AutoNegotiation: false,
		LinkDetected:    attrs.OperState == netlink.OperUp,
		Driver:          "",
		DriverVersion:   "",
		FirmwareVersion: "",
		BusInfo:         "",
		Ethtool:         nil,
		Up:              attrs.Flags&net.FlagUp != 0 && attrs.OperState != netlink.OperDown && attrs.OperState != netlink.OperNotPresent,
	}
	return l
}

func normalizeHardwareAddr(hw net.HardwareAddr) net.HardwareAddr {
	if len(hw) == 0 {
		return nil
	}
	for _, b := range hw {
		if b != 0 {
			cp := make(net.HardwareAddr, len(hw))
			copy(cp, hw)
			return cp
		}
	}
	return nil
}

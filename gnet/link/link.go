package link

import (
	"fmt"
	"net"

	"github.com/sofiworker/gk/gnet/ethtool"
)

// UnknownSpeedMbps 表示无法确定速率时的占位值。
const UnknownSpeedMbps int64 = -1

// Link 描述一个网卡（类似 ip link）。
type Link struct {
	Index        int
	Name         string
	MTU          int
	HardwareAddr net.HardwareAddr
	Flags        net.Flags
	OperState    string
	Up           bool

	SpeedMbps       int64
	Duplex          ethtool.DuplexMode
	AutoNegotiation bool
	LinkDetected    bool

	Driver          string
	DriverVersion   string
	FirmwareVersion string
	BusInfo         string

	Ethtool *ethtool.Info
}

// List 返回当前所有网卡。
func List() ([]Link, error) {
	links, err := listLinks()
	if err != nil {
		return nil, err
	}
	for i := range links {
		attachEthtool(&links[i])
	}
	return links, nil
}

// ByName 通过网卡名获取单个网卡。
func ByName(name string) (*Link, error) {
	if name == "" {
		return nil, fmt.Errorf("link: empty name")
	}
	links, err := List()
	if err != nil {
		return nil, err
	}
	for i := range links {
		if links[i].Name == name {
			return &links[i], nil
		}
	}
	return nil, fmt.Errorf("link %s not found", name)
}

// IsUp 判断网卡是否处于 up。
func IsUp(l Link) bool {
	return l.Up
}

// HasCarrier 判断是否有链路（优先使用 ethtool 信息）。
func HasCarrier(l Link) bool {
	if l.Ethtool != nil {
		return l.Ethtool.LinkDetected
	}
	return l.LinkDetected || l.Up
}

func attachEthtool(l *Link) {
	info, err := ethtool.Get(l.Name)
	if err != nil {
		return
	}

	l.Ethtool = info
	l.Driver = info.Driver
	l.DriverVersion = info.DriverVersion
	l.FirmwareVersion = info.FirmwareVersion
	l.BusInfo = info.BusInfo
	l.SpeedMbps = info.Speed
	l.Duplex = info.Duplex
	l.AutoNegotiation = info.AutoNegotiation
	l.LinkDetected = info.LinkDetected
}

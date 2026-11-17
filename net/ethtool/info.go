package ethtool

import (
	"errors"
	"fmt"
)

// ErrNotSupported 表示当前平台不支持通过 ethtool 获取网卡信息。
var ErrNotSupported = errors.New("ethtool: not supported on this platform")

// DuplexMode 表示网卡的双工模式。
type DuplexMode string

const (
	DuplexUnknown DuplexMode = "unknown"
	DuplexHalf    DuplexMode = "half"
	DuplexFull    DuplexMode = "full"
)

const unknownSpeedMbps int64 = -1

// Info 汇总 ethtool 可获取的基础信息，速度单位为 Mbps，未知速度时为 -1。
type Info struct {
	Driver          string
	DriverVersion   string
	FirmwareVersion string
	BusInfo         string
	Speed           int64
	Duplex          DuplexMode
	AutoNegotiation bool
	LinkDetected    bool
}

// Get 按网卡名获取信息。
func Get(iface string) (*Info, error) {
	if iface == "" {
		return nil, fmt.Errorf("ethtool: empty interface name")
	}
	info, err := getInfo(iface)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("ethtool: nil info returned")
	}
	return info, nil
}

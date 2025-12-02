//go:build linux

package ethtool

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	autonegDisable    = 0x00
	autonegEnable     = 0x01
	duplexCodeHalf    = 0x00
	duplexCodeFull    = 0x01
	duplexCodeUnknown = 0xff
	speedUnknown      = 0xffff
)

const (
	ifreqSize    = int(unsafe.Sizeof(unix.Ifreq{}))
	ifreqDataPad = ifreqSize - unix.IFNAMSIZ - int(unsafe.Sizeof(uintptr(0)))
)

type ifreqData struct {
	Name [unix.IFNAMSIZ]byte
	Data unsafe.Pointer
	_    [ifreqDataPad]byte
}

type ethtoolCmd struct {
	Cmd           uint32
	Supported     uint32
	Advertising   uint32
	Speed         uint16
	Duplex        uint8
	Port          uint8
	PhyAddress    uint8
	Transceiver   uint8
	Autoneg       uint8
	MdioSupport   uint8
	MaxTxPkt      uint32
	MaxRxPkt      uint32
	SpeedHi       uint16
	EthTpMdix     uint8
	EthTpMdixCtrl uint8
	LpAdvertising uint32
	Reserved      [2]uint32
}

type ethtoolValue struct {
	Cmd  uint32
	Data uint32
}

func getInfo(name string) (*Info, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return nil, fmt.Errorf("open socket: %w", err)
	}
	defer unix.Close(fd)

	drv, err := unix.IoctlGetEthtoolDrvinfo(fd, name)
	if err != nil {
		return nil, fmt.Errorf("ioctl get driver info: %w", err)
	}

	cmd := ethtoolCmd{Cmd: unix.ETHTOOL_GSET}
	if err := ioctlEthtool(fd, name, unsafe.Pointer(&cmd)); err != nil {
		return nil, fmt.Errorf("ioctl get link settings: %w", err)
	}

	linkDetected, err := fetchLinkState(fd, name)
	if err != nil {
		return nil, fmt.Errorf("ioctl get link state: %w", err)
	}

	return &Info{
		Driver:          unix.ByteSliceToString(drv.Driver[:]),
		DriverVersion:   unix.ByteSliceToString(drv.Version[:]),
		FirmwareVersion: unix.ByteSliceToString(drv.Fw_version[:]),
		BusInfo:         unix.ByteSliceToString(drv.Bus_info[:]),
		Speed:           parseSpeed(cmd),
		Duplex:          parseDuplex(cmd),
		AutoNegotiation: cmd.Autoneg == autonegEnable,
		LinkDetected:    linkDetected,
	}, nil
}

func ioctlEthtool(fd int, ifName string, data unsafe.Pointer) error {
	if len(ifName) >= unix.IFNAMSIZ {
		return fmt.Errorf("interface name too long: %s", ifName)
	}

	var ifr ifreqData
	copy(ifr.Name[:], ifName)
	ifr.Data = data

	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.SIOCETHTOOL), uintptr(unsafe.Pointer(&ifr))); errno != 0 {
		return errno
	}
	return nil
}

func fetchLinkState(fd int, name string) (bool, error) {
	val := ethtoolValue{Cmd: unix.ETHTOOL_GLINK}
	if err := ioctlEthtool(fd, name, unsafe.Pointer(&val)); err != nil {
		return false, err
	}
	return val.Data != 0, nil
}

func parseSpeed(cmd ethtoolCmd) int64 {
	raw := (uint32(cmd.SpeedHi) << 16) | uint32(cmd.Speed)
	if cmd.Speed == speedUnknown || cmd.SpeedHi == speedUnknown || raw == 0 {
		return unknownSpeedMbps
	}
	return int64(raw)
}

func parseDuplex(cmd ethtoolCmd) DuplexMode {
	switch cmd.Duplex {
	case duplexCodeHalf:
		return DuplexHalf
	case duplexCodeFull:
		return DuplexFull
	case duplexCodeUnknown:
		return DuplexUnknown
	default:
		return DuplexUnknown
	}
}

//go:build windows

package ethtool

import (
	"fmt"
	"net"

	"golang.org/x/sys/windows"
)

const (
	mediaConnectStateUnknown      = 0
	mediaConnectStateDisconnected = 1
	mediaConnectStateConnecting   = 2
	mediaConnectStateConnected    = 3
)

func getInfo(name string) (*Info, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup interface: %w", err)
	}

	var row windows.MibIfRow2
	row.InterfaceIndex = uint32(iface.Index)
	if err := windows.GetIfEntry2Ex(windows.MibIfEntryNormal, &row); err != nil {
		return nil, fmt.Errorf("GetIfEntry2Ex: %w", err)
	}

	linkDetected := row.OperStatus == windows.IfOperStatusUp || row.MediaConnectState == mediaConnectStateConnected

	return &Info{
		Driver:          windows.UTF16ToString(row.Description[:]),
		DriverVersion:   "",
		FirmwareVersion: "",
		BusInfo:         "",
		Speed:           chooseSpeedMbps(row.ReceiveLinkSpeed, row.TransmitLinkSpeed),
		Duplex:          DuplexUnknown,
		AutoNegotiation: false,
		LinkDetected:    linkDetected,
	}, nil
}

func chooseSpeedMbps(rx, tx uint64) int64 {
	chosen := rx
	if tx > chosen {
		chosen = tx
	}
	if chosen == 0 {
		return unknownSpeedMbps
	}
	return int64(chosen / 1_000_000)
}

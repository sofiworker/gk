//go:build windows

package link

import (
	"fmt"
	"net"

	"github.com/sofiworker/gk/gnet/ethtool"
	"golang.org/x/sys/windows"
)

const (
	mediaConnectStateUnknown      = 0
	mediaConnectStateDisconnected = 1
	mediaConnectStateConnecting   = 2
	mediaConnectStateConnected    = 3
)

func listLinks() ([]Link, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	var links []Link
	for _, iface := range ifaces {
		l := Link{
			Index:           iface.Index,
			Name:            iface.Name,
			MTU:             iface.MTU,
			HardwareAddr:    iface.HardwareAddr,
			Flags:           iface.Flags,
			OperState:       "unknown",
			SpeedMbps:       UnknownSpeedMbps,
			Duplex:          ethtool.DuplexUnknown,
			AutoNegotiation: false,
			LinkDetected:    false,
			FirmwareVersion: "",
			DriverVersion:   "",
			BusInfo:         "",
			Ethtool:         nil,
			Driver:          "",
		}

		if row, err := fetchIfRow(iface.Index); err == nil {
			l.OperState = operStatusString(row.OperStatus)
			l.Up = row.OperStatus == windows.IfOperStatusUp
			l.LinkDetected = row.MediaConnectState == mediaConnectStateConnected
			if speed := chooseSpeedMbps(row.ReceiveLinkSpeed, row.TransmitLinkSpeed); speed > 0 {
				l.SpeedMbps = speed
			}
		} else {
			l.Up = iface.Flags&net.FlagUp != 0
		}

		links = append(links, l)
	}

	return links, nil
}

func fetchIfRow(index int) (*windows.MibIfRow2, error) {
	row := windows.MibIfRow2{InterfaceIndex: uint32(index)}
	if err := windows.GetIfEntry2Ex(windows.MibIfEntryNormal, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

func operStatusString(status uint32) string {
	switch status {
	case windows.IfOperStatusUp:
		return "up"
	case windows.IfOperStatusDown:
		return "down"
	case windows.IfOperStatusTesting:
		return "testing"
	case windows.IfOperStatusUnknown:
		return "unknown"
	case windows.IfOperStatusDormant:
		return "dormant"
	case windows.IfOperStatusNotPresent:
		return "not-present"
	case windows.IfOperStatusLowerLayerDown:
		return "lowerlayerdown"
	default:
		return "unknown"
	}
}

func chooseSpeedMbps(rx, tx uint64) int64 {
	chosen := rx
	if tx > chosen {
		chosen = tx
	}
	if chosen == 0 {
		return UnknownSpeedMbps
	}
	return int64(chosen / 1_000_000)
}

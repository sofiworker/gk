package layers

import (
	"fmt"
	"net"
)

type Ethernet struct {
	BaseLayer
	SrcMAC, DstMAC net.HardwareAddr
	EthernetType   LayerType
}

func (e *Ethernet) LayerType() LayerType {
	return LayerTypeEthernet
}

func (e *Ethernet) String() string {
	return fmt.Sprintf("Ethernet %s -> %s Type: %s",
		e.SrcMAC, e.DstMAC, e.EthernetType)
}

package layers

import "errors"

var (
	ErrUnsupportedEtherType = errors.New("layers: unsupported ethernet type")
	ErrUnsupportedProtocol  = errors.New("layers: unsupported protocol")
)

func Decode(data []byte) ([]Layer, error) {
	return DecodeFrom(LayerTypeEthernet, data)
}

func DecodeFrom(layerType LayerType, data []byte) ([]Layer, error) {
	var (
		currentType = layerType
		payload     = data
		result      []Layer
	)

	for {
		layer, err := DecodeLayer(currentType, payload)
		if err != nil {
			return result, err
		}
		result = append(result, layer)

		nextType, nextPayload, ok := nextLayer(layer)
		if !ok || len(nextPayload) == 0 {
			break
		}

		currentType = nextType
		payload = nextPayload
	}
	return result, nil
}

func nextLayer(layer Layer) (LayerType, []byte, bool) {
	switch l := layer.(type) {
	case *Ethernet:
		switch l.EtherType {
		case EthernetTypeIPv4:
			return LayerTypeIPv4, l.Payload(), true
		case EthernetTypeIPv6:
			return LayerTypeIPv6, l.Payload(), true
		default:
			return 0, nil, false
		}
	case *IPv4:
		switch l.Protocol {
		case ProtocolTCP:
			return LayerTypeTCP, l.Payload(), true
		case ProtocolUDP:
			return LayerTypeUDP, l.Payload(), true
		default:
			return 0, nil, false
		}
	case *IPv6:
		switch l.NextHeader {
		case ProtocolTCP:
			return LayerTypeTCP, l.Payload(), true
		case ProtocolUDP:
			return LayerTypeUDP, l.Payload(), true
		default:
			return 0, nil, false
		}
	default:
		return 0, nil, false
	}
}

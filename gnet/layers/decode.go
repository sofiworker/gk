package layers

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
		case EthernetTypeARP:
			return LayerTypeARP, l.Payload(), true
		default:
			return 0, nil, false
		}
	case *IPv4:
		switch l.Protocol {
		case ProtocolTCP:
			return LayerTypeTCP, l.Payload(), true
		case ProtocolUDP:
			return LayerTypeUDP, l.Payload(), true
		case ProtocolICMP:
			return LayerTypeICMP, l.Payload(), true
		default:
			return 0, nil, false
		}
	case *IPv6:
		switch l.NextHeader {
		case ProtocolTCP:
			return LayerTypeTCP, l.Payload(), true
		case ProtocolUDP:
			return LayerTypeUDP, l.Payload(), true
		case ProtocolICMP:
			return LayerTypeICMP, l.Payload(), true
		default:
			return 0, nil, false
		}
	case *UDP:
		// 53 端口的 UDP 载荷按 DNS 解析。
		// UDP payloads on port 53 parse as DNS.
		if l.SrcPort == 53 || l.DstPort == 53 {
			return LayerTypeDNS, l.Payload(), true
		}
		return 0, nil, false
	default:
		return 0, nil, false
	}
}

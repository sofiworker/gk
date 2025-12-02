package layers

import "fmt"

// Parsed 聚合常见协议层，便于直接访问。
type Parsed struct {
	Layers []Layer
	Ether  *Ethernet
	IPv4   *IPv4
	IPv6   *IPv6
	TCP    *TCP
	UDP    *UDP
	Final  Layer // 最后一层
}

// Parse 从以太网开始解码完整链路。
func Parse(data []byte) (*Parsed, error) {
	return ParseFrom(LayerTypeEthernet, data)
}

// ParseFrom 从指定层开始解码。
func ParseFrom(start LayerType, data []byte) (*Parsed, error) {
	ls, err := DecodeFrom(start, data)
	if err != nil {
		return nil, err
	}
	p := &Parsed{Layers: ls}
	for _, l := range ls {
		switch v := l.(type) {
		case *Ethernet:
			p.Ether = v
		case *IPv4:
			p.IPv4 = v
		case *IPv6:
			p.IPv6 = v
		case *TCP:
			p.TCP = v
		case *UDP:
			p.UDP = v
		}
		p.Final = l
	}
	return p, nil
}

// Payload 返回最后一层的载荷数据。
func (p *Parsed) Payload() ([]byte, error) {
	if p == nil || p.Final == nil {
		return nil, fmt.Errorf("layers: no payload available")
	}
	return p.Final.Payload(), nil
}

package bpfparser

import (
	"encoding/binary"
	"fmt"
	"gk/netx/rawcap"
	"net"
)

type PacketFilter struct {
	root *ASTNode
}

func NewPacketFilter(root *ASTNode) *PacketFilter {
	return &PacketFilter{root: root}
}

func (f *PacketFilter) Match(packet *rawcap.Packet) (bool, error) {
	return f.matchNode(f.root, packet)
}

func (f *PacketFilter) matchNode(node *ASTNode, packet *rawcap.Packet) (bool, error) {
	switch node.Type {
	case NodeBinaryOp:
		return f.matchBinaryOp(node, packet)
	case NodeUnaryOp:
		return f.matchUnaryOp(node, packet)
	case NodeProtocol:
		return f.matchProtocol(node, packet)
	case NodeHost:
		return f.matchHost(node, packet)
	case NodeNet:
		return f.matchNet(node, packet)
	case NodePort:
		return f.matchPort(node, packet)
	case NodePortRange:
		return f.matchPortRange(node, packet)
	default:
		return false, nil
	}
}

func (f *PacketFilter) matchBinaryOp(node *ASTNode, packet *rawcap.Packet) (bool, error) {
	left, err := f.matchNode(node.Left, packet)
	if err != nil {
		return false, err
	}
	right, err := f.matchNode(node.Right, packet)
	if err != nil {
		return false, err
	}

	switch node.Operator {
	case TokenAnd:
		return left && right, nil
	case TokenOr:
		return left || right, nil
	default:
		return false, nil
	}
}

func (f *PacketFilter) matchUnaryOp(node *ASTNode, packet *rawcap.Packet) (bool, error) {
	result, err := f.matchNode(node.Left, packet)
	if err != nil {
		return false, err
	}
	return !result, nil
}

func (f *PacketFilter) matchProtocol(node *ASTNode, packet *rawcap.Packet) (bool, error) {
	switch node.Protocol {
	case "tcp":
		//return packet.Layer(layers.LayerTypeTCP) != nil, nil
	case "udp":
		//return packet.Layer(layers.LayerTypeUDP) != nil, nil
	case "icmp":
		//return packet.Layer(layers.LayerTypeICMPv4) != nil ||
		//	packet.Layer(layers.LayerTypeICMPv6) != nil, nil
	case "ip":
		//return packet.Layer(layers.LayerTypeIPv4) != nil ||
		//	packet.Layer(layers.LayerTypeIPv6) != nil, nil
	case "arp":
		//return packet.Layer(layers.LayerTypeARP) != nil, nil
	case "ether":
	//	return packet.Layer(layers.LayerTypeEthernet) != nil, nil
	default:
		return false, nil
	}
	return true, nil
}

func (f *PacketFilter) matchHost(node *ASTNode, packet *rawcap.Packet) (bool, error) {
	return false, nil
}

func (f *PacketFilter) matchNet(node *ASTNode, packet *rawcap.Packet) (bool, error) {
	return false, nil
}

func (f *PacketFilter) matchPort(node *ASTNode, packet *rawcap.Packet) (bool, error) {
	return false, nil
}

func (f *PacketFilter) matchPortRange(node *ASTNode, packet *rawcap.Packet) (bool, error) {
	return false, nil
}

type RawPacketFilter struct {
	root *ASTNode
}

func NewRawPacketFilter(root *ASTNode) *RawPacketFilter {
	return &RawPacketFilter{root: root}
}

func (f *RawPacketFilter) MatchRawPacket(packetData []byte) (bool, error) {
	return f.matchNodeRaw(f.root, packetData)
}

func (f *RawPacketFilter) matchNodeRaw(node *ASTNode, packetData []byte) (bool, error) {
	switch node.Type {
	case NodeBinaryOp:
		left, err := f.matchNodeRaw(node.Left, packetData)
		if err != nil {
			return false, err
		}
		right, err := f.matchNodeRaw(node.Right, packetData)
		if err != nil {
			return false, err
		}

		switch node.Operator {
		case TokenAnd:
			return left && right, nil
		case TokenOr:
			return left || right, nil
		}

	case NodeUnaryOp:
		result, err := f.matchNodeRaw(node.Left, packetData)
		return !result, err

	case NodeProtocol:
		return f.matchProtocolRaw(node, packetData)

	case NodeHost:
		return f.matchHostRaw(node, packetData)

	case NodePort:
		return f.matchPortRaw(node, packetData)
	}

	return false, nil
}

func (f *RawPacketFilter) matchProtocolRaw(node *ASTNode, packetData []byte) (bool, error) {
	if len(packetData) < 14 {
		return false, nil
	}

	etherType := binary.BigEndian.Uint16(packetData[12:14])

	switch node.Protocol {
	case "tcp":
		return etherType == 0x0800 && len(packetData) >= 34 && packetData[23] == 6, nil
	case "udp":
		return etherType == 0x0800 && len(packetData) >= 34 && packetData[23] == 17, nil
	case "ip":
		return etherType == 0x0800 || etherType == 0x86DD, nil
	}

	return false, nil
}

func (f *RawPacketFilter) matchHostRaw(node *ASTNode, packetData []byte) (bool, error) {
	if len(packetData) < 34 {
		return false, nil
	}

	etherType := binary.BigEndian.Uint16(packetData[12:14])

	if etherType == 0x0800 {
		srcIP := net.IP(packetData[26:30])
		dstIP := net.IP(packetData[30:34])
		return srcIP.Equal(node.Address) || dstIP.Equal(node.Address), nil
	}

	return false, nil
}

func (f *RawPacketFilter) matchPortRaw(node *ASTNode, packetData []byte) (bool, error) {
	if len(packetData) < 34 {
		return false, nil
	}

	etherType := binary.BigEndian.Uint16(packetData[12:14])
	protocol := packetData[23]

	if etherType == 0x0800 && (protocol == 6 || protocol == 17) {
		headerLength := (packetData[14] & 0x0F) * 4
		if len(packetData) < int(14+headerLength+4) {
			return false, nil
		}

		start := 14 + headerLength
		srcPort := binary.BigEndian.Uint16(packetData[start : start+2])
		dstPort := binary.BigEndian.Uint16(packetData[start+2 : start+4])

		return int(srcPort) == node.Port || int(dstPort) == node.Port, nil
	}

	return false, nil
}

// FilterFunc 是返回给用户的过滤函数类型
type FilterFunc func(packet []byte) bool

// FilterCompiler 过滤器编译器
type FilterCompiler struct {
}

// CompileFilter 编译 BPF 表达式为过滤函数
func (c *FilterCompiler) CompileFilter(node *ASTNode) (FilterFunc, error) {
	return c.compileNode(node)
}

func (c *FilterCompiler) compileNode(node *ASTNode) (FilterFunc, error) {
	switch node.Type {
	case NodeBinaryOp:
		return c.compileBinaryOp(node)
	case NodeUnaryOp:
		return c.compileUnaryOp(node)
	case NodeProtocol:
		return c.compileProtocol(node)
	case NodeHost:
		return c.compileHost(node)
	case NodeNet:
		return c.compileNet(node)
	case NodePort:
		return c.compilePort(node)
	case NodePortRange:
		return c.compilePortRange(node)
	default:
		return nil, fmt.Errorf("unsupported node type: %v", node.Type)
	}
}

func (c *FilterCompiler) compileBinaryOp(node *ASTNode) (FilterFunc, error) {
	leftFunc, err := c.compileNode(node.Left)
	if err != nil {
		return nil, err
	}
	rightFunc, err := c.compileNode(node.Right)
	if err != nil {
		return nil, err
	}

	switch node.Operator {
	case TokenAnd:
		return func(packet []byte) bool {
			return leftFunc(packet) && rightFunc(packet)
		}, nil
	case TokenOr:
		return func(packet []byte) bool {
			return leftFunc(packet) || rightFunc(packet)
		}, nil
	default:
		return nil, fmt.Errorf("unknown operator: %v", node.Operator)
	}
}

func (c *FilterCompiler) compileUnaryOp(node *ASTNode) (FilterFunc, error) {
	childFunc, err := c.compileNode(node.Left)
	if err != nil {
		return nil, err
	}

	return func(packet []byte) bool {
		return !childFunc(packet)
	}, nil
}

func (c *FilterCompiler) compileProtocol(node *ASTNode) (FilterFunc, error) {
	switch node.Protocol {
	case "tcp":
		return isTCP, nil
	case "udp":
		return isUDP, nil
	case "icmp":
		return isICMP, nil
	case "ip":
		return isIP, nil
	case "arp":
		return isARP, nil
	default:
		return nil, fmt.Errorf("unknown protocol: %s", node.Protocol)
	}
}

func (c *FilterCompiler) compileHost(node *ASTNode) (FilterFunc, error) {
	return func(packet []byte) bool {
		return matchesHost(packet, node.Address)
	}, nil
}

func (c *FilterCompiler) compileNet(node *ASTNode) (FilterFunc, error) {
	return func(packet []byte) bool {
		return matchesNetwork(packet, node.Network)
	}, nil
}

func (c *FilterCompiler) compilePort(node *ASTNode) (FilterFunc, error) {
	return func(packet []byte) bool {
		return matchesPort(packet, node.Port)
	}, nil
}

func (c *FilterCompiler) compilePortRange(node *ASTNode) (FilterFunc, error) {
	return func(packet []byte) bool {
		return matchesPortRange(packet, node.PortMin, node.PortMax)
	}, nil
}

// CompileToFilter 主要公共接口：编译BPF表达式为过滤函数
func CompileToFilter(filterExpr string) (FilterFunc, error) {
	// 1. 解析表达式
	node, err := ParseBPF(filterExpr)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// 2. 编译为过滤函数
	compiler := &FilterCompiler{}
	filterFunc, err := compiler.CompileFilter(node)
	if err != nil {
		return nil, fmt.Errorf("compile error: %w", err)
	}

	return filterFunc, nil
}

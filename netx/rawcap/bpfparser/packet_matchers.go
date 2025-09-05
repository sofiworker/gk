package bpfparser

import (
	"encoding/binary"
	"net"
)

// 基础协议检查函数
func isTCP(packet []byte) bool {
	return getIPProtocol(packet) == 6
}

func isUDP(packet []byte) bool {
	return getIPProtocol(packet) == 17
}

func isICMP(packet []byte) bool {
	return getIPProtocol(packet) == 1
}

func isIP(packet []byte) bool {
	if len(packet) < 14 {
		return false
	}
	etherType := binary.BigEndian.Uint16(packet[12:14])
	return etherType == 0x0800 || etherType == 0x86DD
}

func isARP(packet []byte) bool {
	if len(packet) < 14 {
		return false
	}
	etherType := binary.BigEndian.Uint16(packet[12:14])
	return etherType == 0x0806
}

// 获取IP协议类型
func getIPProtocol(packet []byte) uint8 {
	if len(packet) < 34 {
		return 0
	}
	etherType := binary.BigEndian.Uint16(packet[12:14])
	if etherType == 0x0800 { // IPv4
		return packet[23]
	}
	return 0
}

// 主机匹配
func matchesHost(packet []byte, host net.IP) bool {
	if len(packet) < 34 {
		return false
	}

	etherType := binary.BigEndian.Uint16(packet[12:14])

	if etherType == 0x0800 { // IPv4
		srcIP := net.IP(packet[26:30])
		dstIP := net.IP(packet[30:34])
		return srcIP.Equal(host) || dstIP.Equal(host)
	}

	return false
}

// 网络匹配
func matchesNetwork(packet []byte, network *net.IPNet) bool {
	if len(packet) < 34 {
		return false
	}

	etherType := binary.BigEndian.Uint16(packet[12:14])

	if etherType == 0x0800 { // IPv4
		srcIP := net.IP(packet[26:30])
		dstIP := net.IP(packet[30:34])
		return network.Contains(srcIP) || network.Contains(dstIP)
	}

	return false
}

// 端口匹配
func matchesPort(packet []byte, port int) bool {
	if len(packet) < 34 {
		return false
	}

	etherType := binary.BigEndian.Uint16(packet[12:14])
	protocol := getIPProtocol(packet)

	if etherType == 0x0800 && (protocol == 6 || protocol == 17) {
		headerLength := (packet[14] & 0x0F) * 4
		if len(packet) < int(14+headerLength+4) {
			return false
		}

		start := 14 + headerLength
		srcPort := binary.BigEndian.Uint16(packet[start : start+2])
		dstPort := binary.BigEndian.Uint16(packet[start+2 : start+4])

		return int(srcPort) == port || int(dstPort) == port
	}

	return false
}

// 端口范围匹配
func matchesPortRange(packet []byte, minPort, maxPort int) bool {
	if len(packet) < 34 {
		return false
	}

	etherType := binary.BigEndian.Uint16(packet[12:14])
	protocol := getIPProtocol(packet)

	if etherType == 0x0800 && (protocol == 6 || protocol == 17) {
		headerLength := (packet[14] & 0x0F) * 4
		if len(packet) < int(14+headerLength+4) {
			return false
		}

		start := 14 + headerLength
		srcPort := int(binary.BigEndian.Uint16(packet[start : start+2]))
		dstPort := int(binary.BigEndian.Uint16(packet[start+2 : start+4]))

		return (srcPort >= minPort && srcPort <= maxPort) ||
			(dstPort >= minPort && dstPort <= maxPort)
	}

	return false
}

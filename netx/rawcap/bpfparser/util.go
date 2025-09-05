package bpfparser

import (
	"net"
	"strconv"
	"strings"
	"unicode"
)

// 字符检查工具函数
func isLetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

func isLetterOrDigit(ch rune) bool {
	return isLetter(ch) || isDigit(ch)
}

func isWhitespace(ch rune) bool {
	return unicode.IsSpace(ch)
}

// 网络工具函数
func parseIP(ipStr string) net.IP {
	return net.ParseIP(ipStr)
}

func parseCIDR(cidrStr string) (*net.IPNet, error) {
	_, network, err := net.ParseCIDR(cidrStr)
	return network, err
}

func isValidPort(port int) bool {
	return port >= 0 && port <= 65535
}

func parsePort(portStr string) (int, error) {
	port, err := strconv.Atoi(portStr)
	if err != nil || !isValidPort(port) {
		return 0, err
	}
	return port, nil
}

func parsePortRange(rangeStr string) (int, int, error) {
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		return 0, 0, nil
	}

	min, err := parsePort(parts[0])
	if err != nil {
		return 0, 0, err
	}

	max, err := parsePort(parts[1])
	if err != nil {
		return 0, 0, err
	}

	if min > max {
		return 0, 0, nil
	}

	return min, max, nil
}

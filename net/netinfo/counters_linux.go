//go:build linux

package netinfo

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// IOCounters 返回全部网卡的收发统计。
// IOCounters returns rx/tx counters for every interface.
func IOCounters() ([]IOCountersStat, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, fmt.Errorf("netinfo: open /proc/net/dev: %w", err)
	}
	defer file.Close()

	var out []IOCountersStat
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		fields := strings.Fields(line[i+1:])
		if len(fields) < 16 {
			continue
		}
		num := func(idx int) uint64 {
			v, _ := strconv.ParseUint(fields[idx], 10, 64)
			return v
		}
		out = append(out, IOCountersStat{
			Name:      name,
			RxBytes:   num(0),
			RxPackets: num(1),
			RxErrors:  num(2),
			RxDropped: num(3),
			TxBytes:   num(8),
			TxPackets: num(9),
			TxErrors:  num(10),
			TxDropped: num(11),
		})
	}
	return out, scanner.Err()
}

// ProtoCounters 返回协议统计计数（来自 /proc/net/snmp），
// 键形如 "Ip.InReceives"、"Tcp.ActiveOpens"。
//
// ProtoCounters returns protocol counters (from /proc/net/snmp) keyed like
// "Ip.InReceives" and "Tcp.ActiveOpens".
func ProtoCounters() (map[string]uint64, error) {
	file, err := os.Open("/proc/net/snmp")
	if err != nil {
		return nil, fmt.Errorf("netinfo: open /proc/net/snmp: %w", err)
	}
	defer file.Close()

	out := make(map[string]uint64)
	scanner := bufio.NewScanner(file)
	var (
		proto  string
		header []string
	)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if len(header) == 0 {
			// 表头行：记录协议名（剥冒号）与字段名。
			proto = strings.TrimSuffix(fields[0], ":")
			header = fields[1:]
			continue
		}
		if strings.TrimSuffix(fields[0], ":") != proto {
			continue
		}
		for i := 1; i < len(fields) && i-1 < len(header); i++ {
			if v, err := strconv.ParseUint(fields[i], 10, 64); err == nil {
				out[proto+"."+header[i-1]] = v
			}
		}
		header = nil // 下一个协议块
	}
	return out, scanner.Err()
}

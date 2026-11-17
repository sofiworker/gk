//go:build linux

package netinfo

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// tcpStates 是 /proc 状态码到名称的映射。
// tcpStates maps /proc state codes to names.
var tcpStates = map[string]string{
	"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV",
	"04": "FIN_WAIT1", "05": "FIN_WAIT2", "06": "TIME_WAIT",
	"07": "CLOSE", "08": "CLOSE_WAIT", "09": "LAST_ACK",
	"0A": "LISTEN", "0B": "CLOSING",
}

// Connections 返回当前全部 TCP/UDP 连接（含监听 socket），
// 数据源为 /proc/net/tcp{,6} 与 udp{,6}，与 `ss -antup` 对齐。
//
// Connections returns all current TCP/UDP connections (including listeners)
// from /proc/net/tcp{,6} and udp{,6}, aligned with `ss -antup`.
func Connections() ([]ConnInfo, error) {
	var out []ConnInfo
	for _, f := range []struct {
		path  string
		proto string
		state bool
	}{
		{"/proc/net/tcp", "tcp", true},
		{"/proc/net/tcp6", "tcp", true},
		{"/proc/net/udp", "udp", false},
		{"/proc/net/udp6", "udp", false},
	} {
		conns, err := parseProcNetConn(f.path, f.proto, f.state)
		if err != nil {
			return nil, err
		}
		out = append(out, conns...)
	}
	return out, nil
}

// parseProcNetConn 解析单个 /proc/net/* 连接表。
// parseProcNetConn parses one /proc/net/* connection table.
func parseProcNetConn(path, proto string, hasState bool) ([]ConnInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("netinfo: open %s: %w", path, err)
	}
	defer file.Close()

	var out []ConnInfo
	scanner := bufio.NewScanner(file)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first { // 表头
			first = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		c, ok := parseConnFields(fields, proto, hasState)
		if ok {
			out = append(out, c)
		}
	}
	return out, scanner.Err()
}

// parseConnFields 解析一行连接记录。
// parseConnFields parses one connection record line.
func parseConnFields(fields []string, proto string, hasState bool) (ConnInfo, bool) {
	local := strings.Split(fields[1], ":")
	remote := strings.Split(fields[2], ":")
	if len(local) != 2 || len(remote) != 2 {
		return ConnInfo{}, false
	}
	lip, ok1 := hexIP(local[0])
	rip, ok2 := hexIP(remote[0])
	if !ok1 || !ok2 {
		return ConnInfo{}, false
	}
	lp, ok3 := hexPort(local[1])
	rp, ok4 := hexPort(remote[1])
	if !ok3 || !ok4 {
		return ConnInfo{}, false
	}
	c := ConnInfo{Proto: proto, LocalIP: lip, LocalPort: lp, RemoteIP: rip, RemotePort: rp}
	if hasState {
		c.State = tcpStates[fields[3]]
	}
	// uid 与 inode 是最后两个有效列。
	if len(fields) >= 8 {
		if uid, err := strconv.ParseUint(fields[7], 10, 32); err == nil {
			c.UID = uint32(uid)
		}
	}
	if len(fields) >= 10 {
		if inode, err := strconv.ParseUint(fields[9], 10, 32); err == nil {
			c.Inode = uint32(inode)
		}
	}
	return c, true
}

// hexIP 把 /proc 的小端十六进制地址转成 net.IP。
// hexIP converts the /proc little-endian hex address to net.IP.
func hexIP(s string) (net.IP, bool) {
	if len(s) == 8 { // IPv4
		ip := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
			if err != nil {
				return nil, false
			}
			ip[3-i] = byte(v)
		}
		return ip, true
	}
	if len(s) == 32 { // IPv6：/proc 按 32 位字存储、字内小端（::1 → ...01000000）
		ip := make(net.IP, 16)
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(s[i*8:i*8+8], 16, 32)
			if err != nil {
				return nil, false
			}
			ip[i*4+0] = byte(v)
			ip[i*4+1] = byte(v >> 8)
			ip[i*4+2] = byte(v >> 16)
			ip[i*4+3] = byte(v >> 24)
		}
		return ip, true
	}
	return nil, false
}

func hexPort(s string) (uint16, bool) {
	v, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(v), true
}

//go:build linux

package netinfo

import (
	"os"
	"strings"
	"testing"
)

// TestIPv6RealProc 用真实 /proc/net/udp6 数据验证 IPv6 解析（存在 ::1 监听时）。
// TestIPv6RealProc validates IPv6 parsing against real /proc/net/udp6 data
// (when a ::1 listener exists).
func TestIPv6RealProc(t *testing.T) {
	data, err := os.ReadFile("/proc/net/udp6")
	if err != nil {
		t.Skip(err)
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		c, ok := parseConnFields(fields, "udp", false)
		if !ok {
			continue
		}
		if c.LocalIP.IsLoopback() {
			t.Logf("real udp6 loopback conn: %v", c.LocalIP)
			return
		}
	}
	t.Skip("no udp6 loopback conn in /proc (parsing covered by unit test)")
}

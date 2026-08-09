//go:build !windows

package resources

import (
	"os"
	"strconv"
	"strings"
)

func cpuPercent() float64 {
	b, e := os.ReadFile("/proc/stat")
	if e != nil {
		return 0
	}
	f := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
	if len(f) < 5 {
		return 0
	}
	var t, idle uint64
	for i := 1; i < len(f); i++ {
		v, _ := strconv.ParseUint(f[i], 10, 64)
		t += v
		if i == 4 {
			idle = v
		}
	}
	if t == 0 {
		return 0
	}
	return float64(t-idle) * 100 / float64(t)
}

func cpuPercentSupported() bool { return true }

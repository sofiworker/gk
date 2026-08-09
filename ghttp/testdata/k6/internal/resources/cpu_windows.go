//go:build windows

package resources

func cpuPercent() float64       { return 0 }
func cpuPercentSupported() bool { return false }

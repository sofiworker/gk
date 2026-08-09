// Package resources samples process resource metrics.
package resources

import (
	"runtime"
	"time"
)

// Snapshot 包含系统总体 CPU 使用率和进程资源指标。
// Snapshot contains system-wide CPU utilization and process resource metrics.
type Snapshot struct {
	Timestamp time.Time `json:"timestamp"`
	// CPUPercent 表示系统总体 CPU 使用率，而非进程 CPU 使用率。
	// CPUPercent is system-wide CPU utilization, not process CPU usage.
	CPUPercent          float64 `json:"cpu_percent,omitempty"`
	CPUPercentSupported bool    `json:"cpu_percent_supported"`
	MemoryBytes         uint64  `json:"memory_bytes"`
	FDCount             int     `json:"fd_count,omitempty"`
	Goroutines          int     `json:"goroutines"`
	HeapBytes           uint64  `json:"heap_bytes"`
	GCPauseTotal        uint64  `json:"gc_pause_total_ns"`
}

// Sample 返回当前系统与进程资源的尽力采样结果。
// Sample returns a best-effort snapshot of current system and process resources.
func Sample() Snapshot {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return Snapshot{Timestamp: time.Now(), CPUPercent: cpuPercent(), CPUPercentSupported: cpuPercentSupported(), MemoryBytes: ms.Sys, FDCount: fdCount(), Goroutines: runtime.NumGoroutine(), HeapBytes: ms.HeapAlloc, GCPauseTotal: ms.PauseTotalNs}
}

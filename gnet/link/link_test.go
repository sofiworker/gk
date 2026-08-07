package link

import (
	"runtime"
	"testing"

	"github.com/sofiworker/gk/gnet/ethtool"
)

func TestHasCarrier(t *testing.T) {
	l := Link{}
	if HasCarrier(l) {
		t.Fatalf("expected no carrier for zero link")
	}

	l.LinkDetected = true
	if !HasCarrier(l) {
		t.Fatalf("expected carrier from LinkDetected flag")
	}

	l.Ethtool = &ethtool.Info{LinkDetected: false}
	if HasCarrier(l) {
		t.Fatalf("expected ethtool value to take precedence")
	}
}

func TestIsUp(t *testing.T) {
	l := Link{Up: true}
	if !IsUp(l) {
		t.Fatalf("expected IsUp true")
	}
	l.Up = false
	if IsUp(l) {
		t.Fatalf("expected IsUp false")
	}
}

func TestListLinksStubUnsupported(t *testing.T) {
	// 确保 Unsupported 桩在非支持平台编译并返回错误；linux/windows 通过 build tag 跳过本测试。
	// Ensure the Unsupported stub compiles and errors on unsupported platforms.
	if runtimeGOOS() != "linux" && runtimeGOOS() != "windows" {
		if _, err := listLinks(); err == nil {
			t.Fatalf("expected error on unsupported platform")
		}
	}
}

// runtimeGOOS 拆分为仅供测试的运行时查询，避免在生产引入完整 runtime。
// runtimeGOOS is split for test-only runtime lookup without pulling runtime in production.
var runtimeGOOS = func() string {
	return runtime.GOOS
}

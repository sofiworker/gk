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
	// Ensure Unsupported stub compiles and returns error when built on
	// non-supported platforms. On linux/windows this test is skipped via build tags.
	if runtimeGOOS() != "linux" && runtimeGOOS() != "windows" {
		if _, err := listLinks(); err == nil {
			t.Fatalf("expected error on unsupported platform")
		}
	}
}

// runtimeGOOS is split for test-only runtime lookup without pulling full runtime in production.
var runtimeGOOS = func() string {
	return runtime.GOOS
}

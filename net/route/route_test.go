package route

import (
	"net"
	"runtime"
	"testing"
)

func TestAddDeleteRequireTarget(t *testing.T) {
	if err := Add(Route{}); err == nil {
		t.Fatalf("expected error when dst/gw missing")
	}
	if err := Delete(Route{}); err == nil {
		t.Fatalf("expected error when dst/gw missing")
	}
}

func TestUnsupportedStubRoute(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		if _, err := List(0); err == nil {
			t.Fatalf("expected not supported error")
		}
	}
}

func parseCIDR(c string) *net.IPNet {
	_, n, _ := net.ParseCIDR(c)
	return n
}

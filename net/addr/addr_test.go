package addr

import (
	"net"
	"runtime"
	"testing"
)

func TestAddRequiresIPNet(t *testing.T) {
	if err := Add(Address{}); err == nil {
		t.Fatalf("expected error when IPNet missing")
	}
	if err := Delete(Address{}); err == nil {
		t.Fatalf("expected error when IPNet missing")
	}
}

func TestUnsupportedStub(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		if _, err := List(""); err == nil {
			t.Fatalf("expected not supported error")
		}
		if err := add(Address{IPNet: &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}}); err == nil {
			t.Fatalf("expected not supported error")
		}
	}
}

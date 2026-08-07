package netinfo

import (
	"runtime"
	"testing"
)

func TestInterfacesShape(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("netinfo integration test requires linux")
	}
	ifaces, err := Interfaces()
	if err != nil {
		t.Fatalf("Interfaces failed: %v", err)
	}
	for _, iface := range ifaces {
		if iface.Name == "" {
			t.Fatal("interface name must not be empty")
		}
		if iface.Index <= 0 {
			t.Fatalf("interface %s index = %d, want > 0", iface.Name, iface.Index)
		}
		if len(iface.Addresses) != len(iface.IPv4Addrs)+len(iface.IPv6Addrs) {
			t.Fatalf("interface %s address counts mismatch", iface.Name)
		}
	}
}

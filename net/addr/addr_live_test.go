//go:build linux

package addr

import "testing"

// TestListLive 实测网卡地址列表（netlink 可用时）。
// TestListLive exercises the live address list when netlink is available.
func TestListLive(t *testing.T) {
	addrs, err := List("")
	if err != nil {
		t.Skipf("netlink unavailable: %v", err)
	}
	if len(addrs) == 0 {
		t.Fatal("no addresses returned")
	}
	for _, a := range addrs {
		if a.IfName == "" || a.IPNet == nil || a.IPNet.IP == nil {
			t.Fatalf("malformed address: %+v", a)
		}
	}
	t.Logf("listed %d addresses", len(addrs))
}

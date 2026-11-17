//go:build linux

package link

import "testing"

// TestListLive 实测网卡列表（netlink 可用时）。
// TestListLive exercises the live link list when netlink is available.
func TestListLive(t *testing.T) {
	links, err := List()
	if err != nil {
		t.Skipf("netlink unavailable: %v", err)
	}
	if len(links) == 0 {
		t.Fatal("no links returned")
	}
	for _, l := range links {
		if l.Name == "" || l.Index <= 0 {
			t.Fatalf("malformed link: %+v", l)
		}
	}
	t.Logf("listed %d links", len(links))

	lo, err := ByName("lo")
	if err != nil {
		t.Fatalf("ByName(lo): %v", err)
	}
	if lo.Name != "lo" || !lo.Up {
		t.Fatalf("lo link = %+v", lo)
	}
}

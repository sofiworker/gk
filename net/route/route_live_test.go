//go:build linux

package route

import "testing"

// TestListLive 实测路由表：netlink 权限可用时验证 List 返回自洽数据。
// TestListLive exercises the real routing table: when netlink is available,
// List must return self-consistent data.
func TestListLive(t *testing.T) {
	routes, err := List(0)
	if err != nil {
		t.Skipf("netlink unavailable: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("no routes returned from live table")
	}
	for _, r := range routes {
		if r.Dst != nil && r.Dst.IP == nil {
			t.Fatalf("route with nil Dst.IP: %+v", r)
		}
		if r.IfIndex < 0 {
			t.Fatalf("negative IfIndex: %+v", r)
		}
	}
	t.Logf("listed %d routes", len(routes))
}

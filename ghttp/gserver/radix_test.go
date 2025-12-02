package gserver

import "testing"

func TestCompressedRadixTreeRouteCount(t *testing.T) {
	tree := newCompressedRadixTree()
	if got := tree.routeCount(); got != 0 {
		t.Fatalf("expected 0 routes, got %d", got)
	}

	if err := tree.insert(newRouteEntry("/static", nil)); err != nil {
		t.Fatalf("insert static route: %v", err)
	}
	if err := tree.insert(newRouteEntry("/user/:id", nil)); err != nil {
		t.Fatalf("insert param route: %v", err)
	}

	if got := tree.routeCount(); got != 2 {
		t.Fatalf("expected 2 routes, got %d", got)
	}

	if err := tree.remove("/static"); err != nil {
		t.Fatalf("remove route: %v", err)
	}
	if got := tree.routeCount(); got != 1 {
		t.Fatalf("expected 1 route after removal, got %d", got)
	}
}

package ethtool

import "testing"

func TestGetEmptyName(t *testing.T) {
	if _, err := Get(""); err == nil {
		t.Fatal("expected error for empty interface name")
	}
}

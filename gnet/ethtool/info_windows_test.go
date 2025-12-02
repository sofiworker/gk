//go:build windows

package ethtool

import "testing"

func TestChooseSpeedMbps(t *testing.T) {
	cases := []struct {
		name     string
		rx       uint64
		tx       uint64
		expected int64
	}{
		{name: "rx preferred", rx: 2_000_000, tx: 1_000_000, expected: 2},
		{name: "tx preferred", rx: 1_000_000, tx: 3_000_000, expected: 3},
		{name: "zero", rx: 0, tx: 0, expected: unknownSpeedMbps},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseSpeedMbps(tc.rx, tc.tx); got != tc.expected {
				t.Fatalf("chooseSpeedMbps() = %d, want %d", got, tc.expected)
			}
		})
	}
}

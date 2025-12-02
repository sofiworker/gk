//go:build linux

package ethtool

import "testing"

func TestParseSpeed(t *testing.T) {
	cases := []struct {
		name     string
		cmd      ethtoolCmd
		expected int64
	}{
		{
			name:     "low only",
			cmd:      ethtoolCmd{Speed: 1000, SpeedHi: 0},
			expected: 1000,
		},
		{
			name:     "high only",
			cmd:      ethtoolCmd{Speed: 0, SpeedHi: 1},
			expected: 65536,
		},
		{
			name:     "unknown low",
			cmd:      ethtoolCmd{Speed: speedUnknown, SpeedHi: 0},
			expected: unknownSpeedMbps,
		},
		{
			name:     "zero",
			cmd:      ethtoolCmd{Speed: 0, SpeedHi: 0},
			expected: unknownSpeedMbps,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSpeed(tc.cmd); got != tc.expected {
				t.Fatalf("parseSpeed() = %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestParseDuplex(t *testing.T) {
	cases := []struct {
		name     string
		value    uint8
		expected DuplexMode
	}{
		{name: "half", value: duplexCodeHalf, expected: DuplexHalf},
		{name: "full", value: duplexCodeFull, expected: DuplexFull},
		{name: "unknown code", value: duplexCodeUnknown, expected: DuplexUnknown},
		{name: "unexpected", value: 0x02, expected: DuplexUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cmd := ethtoolCmd{Duplex: tc.value}
			if got := parseDuplex(cmd); got != tc.expected {
				t.Fatalf("parseDuplex() = %s, want %s", got, tc.expected)
			}
		})
	}
}

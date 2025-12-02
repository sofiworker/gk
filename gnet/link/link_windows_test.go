//go:build windows

package link

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestOperStatusString(t *testing.T) {
	cases := []struct {
		status uint32
		want   string
	}{
		{windows.IfOperStatusUp, "up"},
		{windows.IfOperStatusDown, "down"},
		{windows.IfOperStatusTesting, "testing"},
		{windows.IfOperStatusUnknown, "unknown"},
		{windows.IfOperStatusDormant, "dormant"},
		{windows.IfOperStatusNotPresent, "not-present"},
		{windows.IfOperStatusLowerLayerDown, "lowerlayerdown"},
		{9999, "unknown"},
	}
	for _, tc := range cases {
		if got := operStatusString(tc.status); got != tc.want {
			t.Fatalf("operStatusString(%d)=%s want %s", tc.status, got, tc.want)
		}
	}
}

func TestChooseSpeedMbps(t *testing.T) {
	cases := []struct {
		rx, tx uint64
		want   int64
	}{
		{rx: 2_000_000, tx: 1_000_000, want: 2},
		{rx: 0, tx: 3_000_000, want: 3},
		{rx: 0, tx: 0, want: UnknownSpeedMbps},
	}
	for _, tc := range cases {
		if got := chooseSpeedMbps(tc.rx, tc.tx); got != tc.want {
			t.Fatalf("chooseSpeedMbps(%d,%d)=%d want %d", tc.rx, tc.tx, got, tc.want)
		}
	}
}

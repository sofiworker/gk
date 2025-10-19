//go:build !linux && !windows && !darwin && !freebsd && !openbsd && !netbsd
// +build !linux,!windows,!darwin,!freebsd,!openbsd,!netbsd

package rawcap

import "fmt"

func openLive(interfaceName string, cfg Config) (Handle, error) {
	return nil, fmt.Errorf("rawcap: live capture not supported on this platform")
}

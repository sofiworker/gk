//go:build !linux && !windows && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package rawcap

import "fmt"

func openLive(interfaceName string, cfg Config) (Handle, error) {
	return nil, fmt.Errorf("rawcap: live capture not supported on this platform")
}

//go:build !linux && !windows

package ethtool

func getInfo(string) (*Info, error) {
	return nil, ErrNotSupported
}

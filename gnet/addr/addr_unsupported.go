//go:build !linux && !windows

package addr

func list(string) ([]Address, error) {
	return nil, ErrNotSupported
}

func add(Address) error {
	return ErrNotSupported
}

func deleteAddr(Address) error {
	return ErrNotSupported
}

//go:build windows

package route

func listRoutes(int) ([]Route, error) {
	return nil, ErrNotSupported
}

func addRoute(Route) error {
	return ErrNotSupported
}

func deleteRoute(Route) error {
	return ErrNotSupported
}

package route

import (
	"errors"
	"fmt"
	"net"
)

// ErrNotSupported 表示当前平台未实现路由操作。
// ErrNotSupported means route operations are not implemented on this
// platform.
var ErrNotSupported = errors.New("route: not supported on this platform")

// Route 描述一个路由项，字段与 ip route 类似。
// Route describes one route entry, with fields similar to `ip route`.
type Route struct {
	Dst      *net.IPNet
	Src      net.IP
	Gw       net.IP
	IfIndex  int
	IfName   string
	Table    int
	Priority int
	Protocol int
	Scope    int
	Type     int
}

// List 列出路由；family 为地址族（0 代表全部，4 为 IPv4，6 为 IPv6）。
// List lists routes; family is the address family (0 all, 4 IPv4, 6 IPv6).
func List(family int) ([]Route, error) {
	return listRoutes(family)
}

// Add 新增路由。
// Add adds a route.
func Add(r Route) error {
	if r.Dst == nil && r.Gw == nil {
		return fmt.Errorf("route: Dst or Gw required")
	}
	return addRoute(r)
}

// Delete 删除路由。
// Delete removes a route.
func Delete(r Route) error {
	if r.Dst == nil && r.Gw == nil {
		return fmt.Errorf("route: Dst or Gw required")
	}
	return deleteRoute(r)
}

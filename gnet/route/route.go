package route

import (
	"errors"
	"fmt"
	"net"
)

// ErrNotSupported 表示当前平台未实现路由操作。
var ErrNotSupported = errors.New("route: not supported on this platform")

// Route 描述一个路由项，字段与 ip route 类似。
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

// List 列出路由，family 为 netlink.FAMILY_* 常量（0 代表全部）。
func List(family int) ([]Route, error) {
	return listRoutes(family)
}

// Add 新增路由。
func Add(r Route) error {
	if r.Dst == nil && r.Gw == nil {
		return fmt.Errorf("route: Dst or Gw required")
	}
	return addRoute(r)
}

// Delete 删除路由。
func Delete(r Route) error {
	if r.Dst == nil && r.Gw == nil {
		return fmt.Errorf("route: Dst or Gw required")
	}
	return deleteRoute(r)
}

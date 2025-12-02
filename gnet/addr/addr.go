package addr

import (
	"errors"
	"fmt"
	"net"
)

// ErrNotSupported 表示当前平台未实现地址操作。
var ErrNotSupported = errors.New("addr: not supported on this platform")

// Address 描述接口上的一个 IP 地址。
type Address struct {
	IfIndex int
	IfName  string
	IPNet   *net.IPNet
	Peer    *net.IPNet
	Label   string
	Scope   int
	Flags   int
}

// List 列出指定网卡的地址（空字符串表示全部）。
func List(iface string) ([]Address, error) {
	addrs, err := list(iface)
	if err != nil {
		return nil, err
	}
	return addrs, nil
}

// Add 在指定接口上添加地址。
func Add(a Address) error {
	if a.IPNet == nil {
		return fmt.Errorf("addr: IPNet is required")
	}
	return add(a)
}

// Delete 删除指定接口上的地址。
func Delete(a Address) error {
	if a.IPNet == nil {
		return fmt.Errorf("addr: IPNet is required")
	}
	return deleteAddr(a)
}

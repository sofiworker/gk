// Package craft 提供报文构造与注入：layers 序列化组合 + 可插拔注入后端
// （真实网卡 AF_PACKET 注入 / 任意 net.Conn 写入）。
//
// Package craft provides packet crafting and injection: layers serialization
// plus pluggable injectors (real NIC AF_PACKET injection / arbitrary net.Conn
// writes).
package craft

import (
	"net"

	"github.com/sofiworker/gk/gnet/layers"
)

// Injector 注入一个完整报文（L2 帧）。
// Injector injects one complete packet (L2 frame).
type Injector interface {
	Inject(pkt []byte) error
}

// Serialize 组合各层（外→内列出）构造报文，返回完整 L2 帧。
// Serialize assembles layers (outermost-first) into a complete L2 frame.
func Serialize(opts layers.SerializeOptions, ls ...layers.Layer) ([]byte, error) {
	return layers.SerializeLayers(opts, ls...)
}

// connInjector 把报文写入任意连接（压测/仿真场景，无需特权）。
// connInjector writes packets into any connection (load testing / simulation;
// no privileges needed).
type connInjector struct {
	conn net.Conn
}

// NewConnInjector 创建连接注入器。
// NewConnInjector creates a conn injector.
func NewConnInjector(conn net.Conn) Injector {
	return &connInjector{conn: conn}
}

// Inject 写入连接。
// Inject writes to the conn.
func (c *connInjector) Inject(pkt []byte) error {
	for len(pkt) > 0 {
		n, err := c.conn.Write(pkt)
		if err != nil {
			return err
		}
		pkt = pkt[n:]
	}
	return nil
}

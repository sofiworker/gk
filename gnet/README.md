# gnet

网络工具：报文解码（layers）、抓包与转发等。
Network utilities: packet decoding (layers), capture and forwarding.

## 子包一览 / Subpackages

| 子包 / Package | 功能 / Purpose | 平台 / Platforms |
|------|------|------|
| `addr` | 网卡地址增删查 / address add/delete/list | linux / windows / unsupported |
| `capture` | 多网卡实时捕获，输出 pcap/pcapng / multi-interface capture to pcap/pcapng | linux |
| `ethtool` | 网卡驱动/速度/协商信息 / driver/speed/negotiation info | linux / windows / unsupported |
| `forward` | TCP（协议感知）/ UDP 转发桥 / TCP (protocol-aware) / UDP bridges | cross-platform |
| `layers` | 以太网/IPv4/IPv6/TCP/UDP/ICMP 解码 / Ethernet/IP/TCP/UDP/ICMP decoding | cross-platform |
| `link` | 网卡链路信息（含 ethtool 聚合）/ link info (with ethtool) | linux / windows / unsupported |
| `netinfo` | 汇总 link/addr/route/ethtool 的网卡视图 / aggregated interface view | depends on subpackages |
| `packet` | pcap/pcapng/rawcap 包统一适配 / unified packet adapter | cross-platform |
| `pcap` / `pcapng` | 抓包文件读写、BPF 过滤拷贝 / capture file I/O and BPF filtering | cross-platform |
| `rawcap` | 原生 AF_PACKET 实时抓包（Linux）/ native AF_PACKET live capture | linux |
| `route` | 路由表增删查 / route add/delete/list | linux / windows / unsupported |

## 使用示例 / Usage

```go
import "github.com/sofiworker/gk/gnet/layers"
```

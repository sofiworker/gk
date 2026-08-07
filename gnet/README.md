# gnet

[English](README.en.md) | 中文

网络工具：报文解码（layers）、抓包与转发等。

## 子包一览

| 子包 | 功能 | 平台 |
|------|------|----------|
| `addr` | 网卡地址增删查 | linux / windows / unsupported |
| `capture` | 多网卡实时捕获，输出 pcap/pcapng | linux |
| `ethtool` | 网卡驱动/速度/协商信息 | linux / windows / unsupported |
| `forward` | TCP（协议感知）/ UDP 转发桥 | 跨平台 |
| `layers` | 以太网/IPv4/IPv6/TCP/UDP/ICMP 解码 | 跨平台 |
| `link` | 网卡链路信息（含 ethtool 聚合） | linux / windows / unsupported |
| `netinfo` | 汇总 link/addr/route/ethtool 的网卡视图 | 依赖底层子包 |
| `packet` | pcap/pcapng/rawcap 包统一适配 | 跨平台 |
| `pcap` / `pcapng` | 抓包文件读写、BPF 过滤拷贝 | 跨平台 |
| `rawcap` | 原生 AF_PACKET 实时抓包（Linux） | linux |
| `route` | 路由表增删查 | linux / windows / unsupported |

## 使用示例

```go
import "github.com/sofiworker/gk/gnet/layers"
```

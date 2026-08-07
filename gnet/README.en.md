# gnet

English | [中文](README.md)

Network utilities: packet decoding (layers), capture and forwarding.

## Subpackages

| Package | Purpose | Platforms |
|------|------|----------|
| `addr` | address add/delete/list | linux / windows / unsupported |
| `capture` | multi-interface capture to pcap/pcapng | linux |
| `ethtool` | driver/speed/negotiation info | linux / windows / unsupported |
| `forward` | TCP (protocol-aware) / UDP bridges | cross-platform |
| `layers` | Ethernet/IP/TCP/UDP/ICMP decoding | cross-platform |
| `link` | link info (with ethtool) | linux / windows / unsupported |
| `netinfo` | aggregated interface view | depends on subpackages |
| `packet` | unified packet adapter | cross-platform |
| `pcap` / `pcapng` | capture file I/O and BPF filtering | cross-platform |
| `rawcap` | native AF_PACKET live capture | linux |
| `route` | route add/delete/list | linux / windows / unsupported |

## Usage

```go
import "github.com/sofiworker/gk/gnet/layers"
```

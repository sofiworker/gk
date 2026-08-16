# gnet

English | [中文](README.md)

The gk networking foundation module: event-driven server (reactor), packet
decoding (layers), capture and forwarding. **Not a fork of panjf2000/gnet.**

> ⚠️ Under reorganization (pre-v1.0.0): gnet is being restructured into the
> five-domain foundation described in
> `docs/superpowers/specs/2026-08-16-gnet-network-foundation-redesign.md`.
> **M1 is landed** (see below); existing subpackage APIs carry no backward
> compatibility promise and breaking changes will be noted here.

## Structure

```
gpoller (base contract layer, runtime-primitive group)
  Event engine (epoll/kqueue abstraction + io_uring completion engine) + timing wheel + ring buffer
  └ shared by gnet and (later) ghttp

gnet (capability layer)
  ├─ (root)  reactor server: Server/Conn/EventHandler + protocol detection   ← M1 ✓
  ├─ mux / codec / forward        data plane extensions                     ← M2 ✓
  ├─ layers / packet / capture / netinfo   observation plane                ← M3 ✓
  ├─ craft / replay / sniff       active plane                               ← M3 ✓ (replay in M4)
  └─ gateway / actor              assembly & orchestration                   ← M4/M5 ✓
```

## Subpackages

| Package | Purpose | Platforms | Status |
|------|------|----------|--------|
| `gnet` (root) | reactor server (EventHandler/Conn/protocol detection) + **`Listen`: stdlib-compatible engine Listener/Conn** (http/ghttp/grpc plug in via `Serve(ln)` with zero changes) | linux / bsd (Windows in M5) | **M1 ✓ / backing component ✓** |
| `mux` | connection multiplexing: many streams over one conn, yamux-compatible framing, per-stream flow control, FIN half-close, ping keepalive | cross-platform | **M2 ✓** |
| `codec` | stream framing: LengthField/Delimiter/Line/FixedLength/HTTP framers | cross-platform | **M2 ✓** |
| `addr` | address add/delete/list | linux / windows / unsupported | current |
| `capture` | live capture to pcap/pcapng + **BPF expression compiler** (byte-identical to tcpdump -dd) | linux | **M3 ✓** |
| `ethtool` | driver/speed/negotiation info | linux / windows / unsupported | current |
| `forward` | TCP (protocol-aware) / UDP bridges, FIN half-close, splice kernel relay | cross-platform | **M2 ✓** |
| `layers` | Ethernet/IPv4/IPv6/TCP/UDP/ICMP/ARP/DNS decode & **encode** (packet crafting, auto checksums) | cross-platform | **M3 ✓** |
| `link` | link info (with ethtool) | linux / windows / unsupported | current |
| `netinfo` | interface view + **connections/IO counters/proto counters** (/proc sources) | linux / depends on subpackages | **M4 ✓** |
| `packet` | unified packet adapter + layers parse cache (Parse/NetworkLayer/TransportLayer) | cross-platform | **M3 ✓** |
| `pcap` / `pcapng` | capture file I/O and BPF filtering | cross-platform | current |
| `rawcap` | live capture: Linux AF_PACKET / **macOS·BSD /dev/bpf** / **Windows npcap dynamic loading** | linux ✓ / bsd·windows cross-compiled (untested) | **M5 ✓** |
| `route` | route add/delete/list | linux / windows / unsupported | current |
| `craft` | packet crafting & injection: layers assembly + AF_PACKET/conn backends | linux / cross-platform | **M3 ✓** |
| `sniff` | sniffing pipeline: Source→FlowTracker (5-tuple)→Reassembler (TCP reorder)→Analyzer | cross-platform | **M3 ✓** |
| `replay` | traffic replay: timestamp/pps/multiplier pacing, IP+MAC rewrite (incremental checksums), looped replay | cross-platform | **M4 ✓** |
| `gateway` | L4 gateway: rule routing + TCP/UDP port forwarding + protocol-aware hooks | cross-platform | **M4 ✓** |
| `actor` | actor orchestration: Worker conn ownership + serial mailbox (zhenyi zactor model) | cross-platform | **M5 ✓** |

## Root package usage (reactor echo)

```go
package main

import "github.com/sofiworker/gk/gnet"

type echo struct{}

func (echo) OnBoot(_ *gnet.Server) error { return nil }
func (echo) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
    return nil, gnet.ActionNone
}
func (echo) OnTraffic(c gnet.Conn) gnet.Action {
    buf := make([]byte, 4096)
    n, err := c.Read(buf)
    if err != nil {
        return gnet.ActionClose
    }
    if _, err := c.Write(buf[:n]); err != nil {
        return gnet.ActionClose
    }
    return gnet.ActionNone
}
func (echo) OnClose(gnet.Conn, error)                       {}
func (echo) OnTick() (time.Duration, gnet.Action)           { return time.Second, gnet.ActionNone }

func main() {
    s := gnet.New(echo{})
    _ = s.Serve(":9000")
}
```

See `example/gnet_echo` for the full sample.

## Engine switching

```go
// Custom engine (io_uring lands through this hook)
s := gnet.New(h, gnet.WithEngine(eng))

// Busy-poll mode: yield when idle for the lowest wake latency
// (recommended on virtualization where kernel thread wakeups are expensive;
// real hardware can keep the default blocking mode)
s := gnet.New(h, gnet.WithPollInterval(0))
```


## Stdlib backing (Listen)

```go
// Engine-driven, stdlib-compatible listener: accepts and conn I/O run on
// the epoll engine; any stdlib-based server plugs in with zero changes
// (ghttp works the same via Serve(ln)).
ln, err := gnet.Listen(":8080")
if err != nil {
    log.Fatal(err)
}
defer ln.Close()
httpSrv := &http.Server{Handler: echoHandler}
_ = httpSrv.Serve(ln) // consumed directly by net/http
```

## Benchmarks

`go test -bench 'Echo' ./gnet/` compares the gnet reactor against stdlib net
echo round trips. Note: under virtualization kernel thread wakeups can cost
tens of µs and inflate the blocking engine's round trips (the engine's own
wake path is ~2.5µs, see `WithPollInterval(0)`); real hardware wakes in µs.
For throughput scenarios prefer the multi-connection benchmarks.

## Usage (layers)

```go
import "github.com/sofiworker/gk/gnet/layers"
```

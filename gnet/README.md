# gnet

[English](README.en.md) | 中文

gk 的网络底座模块：事件驱动服务器（reactor）、报文解码（layers）、
抓包与转发等。**非 panjf2000/gnet 的 fork。**

> ⚠️ 重构进行中（pre-v1.0.0）：gnet 正按
> `docs/superpowers/specs/2026-08-16-gnet-network-foundation-redesign.md`
> 重组为五域分层底座。**M1 已落地**（见下），现有子包 API 不承诺向后兼容，
> 破坏性变更会在此标注。

## 结构

```
gpoller（基础契约层·运行时原语组）
  事件引擎（epoll/kqueue 抽象 + io_uring completion 引擎）+ 时间轮 + 环形缓冲
  └ 被 gnet 与（未来）ghttp 共用

gnet（能力层）
  ├─ (根包)  reactor 服务器：Server/Conn/EventHandler + 协议识别   ← M1 ✓
  ├─ mux / codec / forward        数据面扩展                          ← M2 ✓
  ├─ layers / packet / capture / netinfo  观测面                     ← M3 ✓
  ├─ craft / replay / sniff       主动面                              ← M3 ✓（replay 待 M4）
  └─ gateway / actor              拼装与编排                          ← M4/M5 ✓
```

## 子包一览

| 子包 | 功能 | 平台 | 状态 |
|------|------|----------|------|
| `gnet`（根） | reactor 服务器（EventHandler/Conn/协议识别）+ **`Listen`：标准库兼容的引擎 Listener/Conn**（http/ghttp/grpc 经 `Serve(ln)` 零改动接入） | linux / bsd（Windows 见 M5） | **M1 ✓ / 垫底组件 ✓** |
| `mux` | 链路复用：单连接多流、yamux 兼容帧、逐流窗口流控、FIN 半关闭、Ping keepalive | 跨平台 | **M2 ✓** |
| `codec` | 流式分帧：LengthField/Delimiter/Line/FixedLength/HTTP 五种分帧器 | 跨平台 | **M2 ✓** |
| `addr` | 网卡地址增删查 | linux / windows / unsupported | 现状 |
| `capture` | 实时捕获输出 pcap/pcapng + **BPF 表达式编译器**（与 tcpdump -dd 逐字节一致） | linux | **M3 ✓** |
| `ethtool` | 网卡驱动/速度/协商信息 | linux / windows / unsupported | 现状 |
| `forward` | TCP（协议感知）/ UDP 转发桥、FIN 半关闭、splice 内核转发 | 跨平台 | **M2 ✓** |
| `layers` | 以太网/IPv4/IPv6/TCP/UDP/ICMP/ARP/DNS 解码与**编码**（构造报文、自动校验和） | 跨平台 | **M3 ✓** |
| `link` | 网卡链路信息（含 ethtool 聚合） | linux / windows / unsupported | 现状 |
| `netinfo` | 网卡视图汇总 + **连接表/网卡统计/协议计数**（/proc 数据源） | linux / 依赖底层子包 | **M4 ✓** |
| `packet` | 包统一适配 + layers 解析缓存（Parse/NetworkLayer/TransportLayer） | 跨平台 | **M3 ✓** |
| `pcap` / `pcapng` | 抓包文件读写、BPF 过滤拷贝 | 跨平台 | 现状 |
| `rawcap` | 实时抓包：Linux AF_PACKET / **macOS·BSD /dev/bpf** / **Windows npcap 动态加载** | linux ✓ / bsd·windows 交叉编译验证（未实测） | **M5 ✓** |
| `route` | 路由表增删查 | linux / windows / unsupported | 现状 |
| `craft` | 报文构造与注入：layers 组合 + AF_PACKET/conn 双后端 | linux / 跨平台 | **M3 ✓** |
| `sniff` | 嗅探管线：Source→FlowTracker（五元组）→Reassembler（TCP 重组）→Analyzer | 跨平台 | **M3 ✓** |
| `replay` | 流量回放：时间戳/pps/倍速 pacing、IP+MAC 重写（增量校验和）、循环回放 | 跨平台 | **M4 ✓** |
| `gateway` | L4 网关：规则路由 + TCP/UDP 端口转发 + 协议感知钩子 | 跨平台 | **M4 ✓** |
| `actor` | actor 编排：Worker 独占连接 + 邮箱串行处理（zhenyi zactor 模型） | 跨平台 | **M5 ✓** |

## 根包使用示例（reactor echo）

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

完整示例见 `example/gnet_echo`。

## 引擎切换

```go
// 自定义引擎（io_uring 落地后经此注入）
s := gnet.New(h, gnet.WithEngine(eng))

// busy-poll 模式：空闲让出 CPU 换取最低唤醒延迟
// （虚拟化等内核线程唤醒昂贵的环境推荐；真实硬件默认阻塞模式即可）
s := gnet.New(h, gnet.WithPollInterval(0))
```


## 标准库垫底（Listen）

```go
// 引擎驱动的标准库兼容 Listener：accept/读写走 epoll 引擎，
// 任何标准库 server 零改动接入（ghttp 同理经 Serve(ln)）。
ln, err := gnet.Listen(":8080")
if err != nil {
    log.Fatal(err)
}
defer ln.Close()
httpSrv := &http.Server{Handler: echoHandler}
_ = httpSrv.Serve(ln) // net/http 直接消费
```

## 基准

`go test -bench 'Echo' ./gnet/` 对比 gnet reactor 与标准库 net 的回显往返。
注意：虚拟化环境下内核线程唤醒可达数十 µs，会放大阻塞引擎的往返延迟
（引擎自身唤醒路径约 2.5µs，见 `WithPollInterval(0)` 模式）；真实硬件上
为 µs 级。吞吐型场景建议用多连接并发基准。

## 使用示例（layers）

```go
import "github.com/sofiworker/gk/gnet/layers"
```

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
| `pcap` | 统一的抓包文件读写、格式自动识别、BPF 过滤拷贝（pcap / pcapng） | 跨平台 | 现状 |
| `rawcap` | 实时抓包：Linux AF_PACKET / **macOS·BSD /dev/bpf** / **Windows npcap 动态加载** | linux ✓ / bsd·windows 交叉编译验证（未实测） | **M5 ✓** |
| `route` | 路由表增删查 | linux / windows / unsupported | 现状 |
| `craft` | 报文构造与注入：layers 组合 + AF_PACKET/conn 双后端 | linux / 跨平台 | **M3 ✓** |
| `sniff` | 嗅探管线：Source→FlowTracker（五元组）→Reassembler（TCP 重组）→Analyzer | 跨平台 | **M3 ✓** |
| `replay` | 流量回放：时间戳/pps/倍速 pacing、IP+MAC 重写（增量校验和）、循环回放 | 跨平台 | **M4 ✓** |
| `gateway` | L4 网关：规则路由 + TCP/UDP 端口转发 + 协议感知钩子 | 跨平台 | **M4 ✓** |
| `actor` | actor 编排：Worker 独占连接 + 邮箱串行处理（zhenyi zactor 模型） | 跨平台 | **M5 ✓** |

## 抓包文件统一入口

所有抓包文件格式都封装在 `github.com/sofiworker/gk/gnet/pcap` 内。
`NewReader(io.Reader)` 根据文件头自动识别 classic pcap（微秒/纳秒、大小端）和
pcapng，不要求输入支持 seek；两种格式都返回同一个 `Packet` 类型。
`NewWriter` 默认输出 classic pcap，通过 `WithFormat(pcap.FormatPCAPNG)` 选择 pcapng。

```go
reader, err := pcap.NewReader(input)
if err != nil {
    return err
}
pkt, err := reader.ReadPacket()
if err != nil {
    return err
}

writer, err := pcap.NewWriter(output, pcap.WithFormat(pcap.FormatPCAPNG))
if err != nil {
    return err
}
defer writer.Close()
return writer.WritePacketData(pkt.Data, pkt.Timestamp)
```

上例将报文字节写入默认接口；需要保留原始长度、接口编号或选项时，使用
`WritePacket(*pcap.Packet)`。`Packet` 包含 `InterfaceID`、`LinkType`、
`CapturedLen`、`OriginalLen`、`Options` 和原有的 classic `Header`。
pcapng 可通过 `AddInterface` 注册多个接口（编号从零开始）；未注册接口时首次向
接口零写入会使用 `WithLinkType`、`WithSnapLen`、`WithTimestampResolution` 自动注册。
classic pcap 仅支持接口零。

`InterfaceInfo` 查询已读取的接口信息；pcapng 接口编号在每个 Section 内独立编号。
`NextBlock`、`CurrentSection`、块类型和 Section/接口选项均位于 `pcap` 包。
classic pcap 调用 `NextBlock` 返回 `ErrUnsupportedFormat`；`Header` 对 pcapng 返回零值，
应改用 `CurrentSection` 和 `InterfaceInfo` 查询元数据。
`FilterCopy` 自动识别并保持输入格式，保留 pcapng 的 Section、接口、报文选项和未知块。
Reader 不关闭输入；Writer 的 `Close` 会刷新缓冲并关闭底层 `io.Closer`，
`FilterCopy` 也会关闭支持 `io.Closer` 的输出。并发写入需要调用方同步。
后续增加格式时，其编解码实现和格式分派也放在该包内，上层共用读写和报文接口。

**迁移说明（破坏性变更）：** 原 `gnet/pcapng` 包已移除。将导入路径改为
`gnet/pcap`，`NewReader` 现在返回 `(*Reader, error)`；创建 pcapng Writer 时增加
`WithFormat(FormatPCAPNG)`，将 `WritePacket(id, data, ts, opts...)` 改为
`WritePacket(&pcap.Packet{InterfaceID: id, Data: data, Timestamp: ts, Options: opts})`。
原 pcapng 的块、错误、Section 和接口配置名称保持不变，统一使用 `pcap` 前缀。
`AddInterface` 的 link type 参数统一为 `uint32`（pcapng 值仍须在 `uint16` 范围内）。
`capture.Format` 及其常量转为统一格式类型的别名；`packet.FromPCAP` 和
`sniff.NewPcapSource` 均支持两种格式，原 `FromPCAPNG`、`NewPcapngSource` 保留为别名入口。

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

# gnet 网络底座重构设计

> 状态：2026-08-16 提出，待 review。依赖政策前置条件见
> `2026-08-16-gk-module-dependency-policy-gpoller-amendment.md`（gpoller 入基础层）。

## 1. 定位

gnet 是 **purego（无 cgo）的网络底座库**，补充标准库 `net` 的不足，覆盖：
事件驱动服务器、链路复用、协议编解码、抓包、报文构造、流量回放、嗅探、
L4 网关、系统网络信息。大而全，但按域解耦、DAG 单向依赖、按需引用。

## 2. 调研依据（摘要与来源）

| 结论 | 来源 |
|------|------|
| io_uring 在 Go 网络框架仍是前沿区（gnet v2 未实现 TODO、netpoll 不做），应作旁路引擎 + 内核版本回退 | <https://github.com/panjf2000/gnet/blob/master/README_ZH.md>、<https://github.com/tokio-rs/tokio-uring/blob/master/DESIGN.md> |
| readiness 与 completion 双模型：底座用回调式 readiness，completion 作第二接口并行演进 | <https://github.com/panjf2000/gnet/blob/master/pkg/netpoll/netpoll.go>、<https://github.com/cloudwego/netpoll/blob/develop/poll.go>、<https://github.com/tokio-rs/mio/blob/master/src/poll.rs> |
| Windows 不自研 readiness 层（runtime 已整合 IOCP+AFD），netpoller 即最优 fallback | <https://github.com/golang/go/blob/master/src/runtime/netpoll_windows.go> |
| 复用层硬参考 yamux：12B 大端帧头、Data/WindowUpdate/Ping/GoAway 四帧、SYN/ACK/FIN/RST、逐流 256KB 窗口、FIN 半关闭 | <https://github.com/hashicorp/yamux/blob/master/spec.md> |
| BPF 表达式编译器无成熟纯 Go 实现（x/net/bpf 只是汇编器、gopacket#851 未实现）；首选自研 mini 子集 | <https://github.com/google/gopacket/issues/851>、<https://pkg.go.dev/golang.org/x/net/bpf> |
| 抓包：gopacket afpacket/pcapgo 纯 Go 可对标；Windows npcap 动态加载、macOS /dev/bpf 自行封装 | <https://github.com/google/gopacket/tree/master/afpacket> |
| 构造：Scapy 层堆叠 + gopacket SerializeBuffer；回放：tcpreplay 三件套；嗅探：Suricata/Zeek 管线；网关：gVisor netstack（纯 Go，含 nat） | <https://scapy.readthedocs.io/>、<https://tcpreplay.appneta.com/>、<https://deepwiki.com/OISF/suricata/3-packet-processing-pipeline>、<https://github.com/google/gvisor> |
| 系统网络信息：gopsutil 的 /proc + 纯 Go netlink 数据源 | <https://github.com/shirou/gopsutil/tree/master/net> |
| 组织原则：契约包零实现、内核最小 + 旁路独立、单一编排中心、抽象+默认实现+钩子、传输/复用/加密/协议四层接口解耦 | <https://github.com/libp2p/go-libp2p/tree/master/core>、<https://netty.io/>、<https://github.com/aiyang-zh/zhenyi-base> |

## 3. 设计原则

1. **契约与实现分离**：每域先定小接口，默认实现与接口同包；上层只依赖接口。
2. **内核最小、旁路可插拔**：gpoller/reactor 内核只做事件+连接+定时+缓冲；
   io_uring、抓包、回放、嗅探、网关均为旁路，不 import 即零成本。
3. **net.Conn 兼容是硬要求**：mux、codec 与第三方生态（http/quic/yamux）
   必须能直接吃 reactor Conn。
4. **DAG 单向依赖**：域间只准向下引用；reactor 热路径零解析零嗅探；
   拼装只发生在顶层域。
5. **purego 到 syscall 为止**：`x/sys` + `syscall.Syscall6`；Windows npcap
   走 `NewLazyDLL`（需防 DLL 劫持）。

## 4. 分层架构

```
L0 基础契约层（政策修订后）
   gerr · gretry · grx · gcompress · gcrypt
   └ gpoller（运行时原语组）：事件引擎 + 时间轮 + 缓冲原语
        仅依赖 stdlib/x/sys；gnet 的直接底座；ghttp 等标准库 server
        经 gnet 的标准库兼容组件（net.Listener/net.Conn）由用户装配

L1 gnet 能力层 —— 五域 DAG，只准向下引
────────────────────────────────────────────────
  拼装域（顶层，可横跨引用，禁止被下层 import）
   ├ gateway   L4 网关：规则路由 + 端口转发 + 协议感知钩子
   │           （L3/TUN 可选集成 gVisor netstack，单独评审）
   └ packet    统一报文模型（现状升级：承载解析缓存与构造产物）

  主动面域
   ├ replay    回放：pcap/pcapng → Injector，pacing/倍速/IP+MAC 重写/循环
   ├ craft     报文构造与注入：layers 编码 + checksum + 发送后端
   └ sniff     嗅探管线：Source→FlowTracker(五元组)→Reassembler
               (TCP 重组)→Analyzer 回调

  数据面域
   ├ gnet(根)  reactor：Server/Conn/EventHandler + 协议识别升级
   ├ mux       链路复用：Muxer/Stream + yamux 式默认实现 + 自定义帧后端
   ├ codec     编解码：Framer + 五种内置分帧器 + 零拷贝缓冲
   └ forward   TCP/UDP 转发桥（net.Conn 引擎兜底 + reactor/splice 引擎）

  观测面域
   ├ layers    dissector 注册表 + 解码/编码/checksum（补 ARP/VLAN/DNS/HTTP…）
   ├ capture   统一捕获：live/offline + mini BPF 表达式编译器 + 多平台后端
   ├ netinfo   gopsutil 式视图：连接表/网卡统计/协议计数 + link/addr/route/ethtool
   └ rawcap · pcap · pcapng   后端与格式（现状保留，rawcap 增补多平台）

  编排（P1 薄层）
   └ actor     actor 编排：建立在 reactor Conn 之上的邮箱调度
```

依赖示例：`gateway → {forward, sniff, craft, mux}`；`sniff → {capture, layers, packet}`；
`replay → {craft, pcap, pcapng}`；`craft → {layers, rawcap}`；`forward → {gnet根, mux}`；
`gnet根 → {gpoller, codec}`；`layers/pcap/pcapng` 纯 stdlib。

## 5. 核心接口草案

### 5.1 gpoller（事件引擎，基础契约层）

```go
// 平台矩阵：linux=epoll(ET)；darwin/bsd=kqueue；windows=stdlib netpoller；
// 兜底 poll；io_uring 仅作旁路 CompletionEngine。
type Event struct {
    FD                      int
    Readable, Writable, Hup bool
    Err                     error
}
type Handler func(Event)

// 必须：readiness 引擎（epoll/kqueue/netpoller 三后端）
type EventEngine interface {
    AddRead(fd int, h Handler) error
    AddWrite(fd int, h Handler) error
    Mod(fd int, read, write bool) error
    Delete(fd int) error
    Poll() error                  // 阻塞循环，一引擎一 goroutine
    Wake(fn func()) error         // 跨 goroutine 任务投递：eventfd / EVFILT_USER
    Close() error
}

// 可选：completion 引擎（io_uring；内核<5.6 时 WithEngine 自动回退）
type Op struct{ Kind OpKind; FD int; Buf []byte } // Read/Write/Accept/Connect/Timeout
type Result struct{ N int; Err error }
type CompletionEngine interface {
    Submit(op Op, cb func(Result)) (uint64, error)
    Cancel(id uint64) error
    Close() error
}
```

同包附：**时间轮**（连接级超时 O(1) 插删；gnet v2 用 `time.Timer` 退化的教训
不复刻）+ **缓冲原语**（ring/elastic buffer + netpoll 式零拷贝
`Next/Peek/Skip/Release` Reader）。

### 5.2 reactor 核心 + 协议识别

```go
type EventHandler interface {
    OnBoot(s *Server) error
    OnOpen(c Conn) (out []byte, action Action)
    OnTraffic(c Conn) (action Action)
    OnClose(c Conn, err error)
    OnTick() (delay time.Duration, action Action)
}
type Conn interface {
    net.Conn                    // 硬兼容：mux/codec/http 全部直接可用
    ID() int64
    Context() context.Context
}

func New(h EventHandler, opts ...ServerOption) *Server
// WithEngine(gpoller.EventEngine)      ← epoll/kqueue/io_uring 切换点
// WithCodec(codec.Framer)              ← 自定义协议接入口
// WithProtoMatcher(proto.Matcher, ...) ← 首字节协议识别升级
// WithLogger / WithTracer               ← 注入点，核心零 glog/gotel 依赖

// 协议识别升级（首字节窥探 + 已读字节回灌）：
type Matcher func(head []byte) bool
func DetectProtocol(conn net.Conn, peek int, matchers []NamedMatcher) (name string, nc net.Conn, err error)
```

### 5.3 mux（链路复用）

```go
type Stream interface {
    io.Reader
    io.Writer
    StreamID() uint32
    Close() error            // FIN + 丢弃未读
    CloseWrite() error       // 半关闭，仍可读
    CloseRead() error
    SetDeadline(t time.Time) error
    SetReadDeadline(t time.Time) error
    SetWriteDeadline(t time.Time) error
}

type Muxer interface {
    OpenStream(ctx context.Context) (Stream, error)
    AcceptStream(ctx context.Context) (Stream, error)
    Ping() (time.Duration, error)   // keepalive + RTT
    NumStreams() int
    GoAway() error                  // 拒新流，排空存量
    Close() error
    IsClosed() bool
}
```

默认实现按 yamux 帧规格（12B 头/四类帧/逐流窗口/FIN 半关闭/Ping keepalive），
`WithWindowSize/WithKeepAlive/WithFramer` 走 WithFunc；`WithFramer` 预留自定义
帧后端（HTTP2 帧层、私有协议复用）。mux 接受任意 `net.Conn`——reactor Conn、
TLS Conn、回放 Conn 均可叠加（libp2p「连接升级链」的 Go 化）。

### 5.4 codec（编解码管线）

放弃 Netty 双向 handler 上下文，退化为无状态分帧器（对齐 `bufio.SplitFunc`
语义，`need` 显式表达半包）：

```go
type Framer interface {
    Encode(msg []byte) ([]byte, error)
    Split(data []byte, atEOF bool) (adv, frame, need int, err error)
}
```

内置五种：`LengthFieldFramer`（参数语义同 Netty LengthFieldBasedFrameDecoder：
offset/lengthFieldLength/adjustment/strip）、`DelimiterFramer`、`LineFramer`、
`FixedLengthFramer`、`HTTPFramer`（Content-Length + chunked 状态机）。

### 5.5 观测面

```go
// layers：补编码与校验（现状零序列化、checksum 只读）
type Encoder interface{ Serialize(b *SerializeBuffer) error }
func InternetChecksum(b []byte) uint16
// IPv4/IPv6 实现 SetNetworkLayerForChecksum（Scapy 语义的自动校验和）

// capture：统一入口 + mini BPF 表达式编译器
type Expr string // "tcp port 80 and host 10.0.0.1"
func CompileExpr(e Expr, linkType int) ([]bpf.Instruction, error) // 自研子集语法
// WithExpr("tcp port 80")；编译后端做成接口，未来可插 libpcap 全语法移植

// netinfo：gopsutil 式视图（/proc + 纯 Go netlink，零 cgo）
func Connections() ([]ConnInfo, error)       // /proc/net/tcp* + sock_diag
func IOCounters() ([]IOCountersStat, error)  // /proc/net/dev
func ProtoCounters() (map[string]uint64, error) // /proc/net/snmp（键如 "Tcp.ActiveOpens"）
```

### 5.6 主动面（craft / replay / sniff）

```go
// craft：构造→注入
type Injector interface{ Inject(pkt []byte) error } // AF_PACKET send / npcap / conn 写入

// replay：tcpreplay 三件套的库化
func NewReplayer(src Reader, dst Injector, opts ...ReplayOption) (*Replayer, error)
// WithPacing(pps) / WithMultiplier(2) / WithRewrite(srcIP, dstIP, srcMAC, dstMAC) / WithLoop(n)

// sniff：Suricata/Zeek 管线简化
type Source interface{ Next() (*packet.Packet, error) }       // capture/文件/replay 统一
type FlowTracker interface{ Track(p *packet.Packet) *Flow }   // 五元组流表
type Reassembler interface{ Push(f *Flow, seg []byte) ([][]byte, error) } // TCP 重组
type Analyzer interface{ OnFlowEvent(ev FlowEvent) }          // 新流/数据/结束回调
```

### 5.7 拼装面（gateway / forward / actor）

- **gateway v1 = L4 拼装**：规则表（目标→上游）+ 端口转发 + 可选协议感知改写
  （forward handler）+ 嗅探钩子（镜像流给 sniff）；L3/TUN = 可选集成
  gVisor netstack（依赖重，单独评审）；
- **forward**：保留 net.Conn 双 goroutine 引擎做跨平台兜底，新增 reactor + splice
  引擎做 Linux 热路径，外 API 不变；
- **actor**（P1）：建立在 reactor Conn 之上（邮箱 + 连接所有权），先不抽顶层模块。

## 6. 能力总目录

| 能力 | 落位 | 业界参照 | 里程碑 |
|------|------|----------|--------|
| 事件引擎（epoll/kqueue/netpoller/io_uring 切换） | gpoller | gnet v2 / netpoll / mio / tokio-uring | M1 |
| 时间轮 + 零拷贝缓冲 | gpoller | gnet pkg/buffer、netpoll LinkBuffer | M1 |
| 事件驱动服务器 | gnet 根 | panjf2000/gnet、zhenyi zreactor | M1 |
| 协议识别升级 | gnet 根 | TLS SNI、libp2p early-negotiation | M2 |
| 链路复用 | gnet/mux | yamux / smux / quic-go | M2 |
| 编解码管线 | gnet/codec | Netty Pipeline、gnet ICodec | M2 |
| TCP/UDP 转发 | gnet/forward | 现状 + splice | M2 |
| 协议解析（解码/编码/checksum） | gnet/layers | gopacket layers、Scapy | M3 |
| 统一报文模型 | gnet/packet | 现状升级 | M3 |
| 抓包（live/offline + BPF 表达式） | gnet/capture | gopacket afpacket/pcapgo | M3 |
| 报文构造与注入 | gnet/craft | Scapy + gopacket SerializeBuffer | M3 |
| 流量嗅探（流表/TCP 重组/回调） | gnet/sniff | Suricata/Zeek 管线 | M3 |
| 流量回放 | gnet/replay | tcpreplay 三件套 | M4 |
| L4 网关 / 用户态栈 | gnet/gateway | gVisor netstack、tun2socks | M4 |
| 系统网络信息 | gnet/netinfo | gopsutil net | M4 |
| actor 编排 | gnet/actor | zhenyi zactor | M5 |
| io_uring completion 引擎 | gpoller | iceber/iouring-go、tokio-uring | M5 |

## 7. 平台矩阵（README 表格）

| 平台 | 事件引擎 | 抓包 | 构造注入 |
|------|----------|------|----------|
| Linux | epoll（一等）→ io_uring（可选） | AF_PACKET（已有 rawcap） | AF_PACKET send |
| macOS/BSD | kqueue | /dev/bpf*（自研 syscall 封装） | 同左 |
| Windows | stdlib netpoller（勿自研） | npcap NewLazyDLL（防劫持） | npcap |

## 8. 里程碑与验收

- **M1 数据面地基**：gpoller（三后端 + 时间轮 + 缓冲）+ reactor echo 服务器。
  验收：echo 基准对齐 gnet 数量级；GOOS=linux/darwin/windows 交叉编译绿；
  无 g* 依赖（gpoller）。
- **M2 数据面扩展**：codec + mux（yamux 式）+ 协议识别 + forward reactor/splice 引擎。
  验收：复用层帧兼容 yamux 规格测试；半关闭语义单测。
- **M3 观测与主动面**：layers 编码/checksum/补协议 + packet + craft + capture
  （含 mini BPF 编译器）+ sniff。验收：构造包被 tcpdump/wireshark 正确解析；
  `"tcp port 80"` 编译结果与 libpcap 对齐（对照测试）。
- **M4 拼装与系统**：replay + netinfo 扩充 + gateway（L4）。
  验收：回放包序/倍速准确；连接表与 `ss -ant` 对齐。
- **M5 平台与编排**：actor + io_uring 旁路 + Windows/macOS 捕获后端 +
  标准库组件垫底：gnet 提供 epoll `net.Listener` + 引擎驱动的
  `net.Conn`（2026-08-16 决策：走标准库接口、零改 ghttp），任意标准库
  server（net/http/ghttp/grpc）经 `Serve(ln)` 自选接入。

## 9. 风险与待决

1. **io_uring**：连 gnet 都未实现，定位「旁路 + 基准门槛」，不阻塞 M1；
   内核 <5.6 自动回退 epoll。
2. **BPF 编译器**：自研子集（host/net/port/proto + and/or/not）覆盖 90% 场景；
   全语法留可插拔后端（BSD 许可 libpcap 移植 / wasm2go 转译，均需依赖评审）。
3. **gVisor netstack 依赖重**：gateway L4 不依赖它；L3 集成单独评审。
4. **Windows 范围**：readiness 不自研（netpoller 兜底）；npcap 加载需路径校验。
5. **破坏性**：pre-v1.0 允许重构；README/CHANGELOG 必须显式标注
   （AGENTS.md 硬要求）。
6. **命名**：`gpoller` 待评审（备选 `gpoll`/`gevent`）；gnet 根包定位
   「reactor 核心」待确认（备选：根包为门面、reactor 下沉 `gnet/reactor`）。
7. **layers 注册表升级**：现有全局 `RegisterLayerDecoder` 注册表保留，
   编码器（Encoder）与 checksum 接口并入同一注册体系。

## 10. 实施顺序

1. 依赖政策修订评审通过（前置）；
2. M1 起步：gpoller（epoll + 时间轮 + 缓冲）→ reactor echo → 基准；
3. 每个里程碑按仓库惯例：本 spec 更新进度 → review → 实现 → 测试/lint → 提交
   （未经授权不推送）。

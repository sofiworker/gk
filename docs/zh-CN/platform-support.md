# 平台支持

[English](../platform-support.md) | **简体中文** | [文档目录](README.md)

多数工具包使用跨平台 Go API。底层网络通过 build tag 选择实现；
能够编译不代表当前平台支持每个操作。

| 能力 | Linux | macOS / BSD | Windows |
| --- | --- | --- | --- |
| HTTP 与通用工具 | Go 实现 | Go 实现 | Go 实现 |
| Reactor/readiness 引擎 | epoll | kqueue | 返回 `poller.ErrNotSupported` |
| Completion 引擎 | io_uring，受内核能力限制 | 不支持 | 不支持 |
| 实时原始抓包 | AF_PACKET | 受支持系统上的 BPF | Npcap DLL |
| pcap/pcapng 文件处理 | 跨平台 | 跨平台 | 跨平台 |
| 网络管理 | 按能力提供实现 | 按能力提供实现 | 按能力提供实现 |

使用地址、路由、链路或 ethtool 操作前，检查对应源码的 build tag。
具体子包见[网络说明](networking.md)，事件引擎见 [Poller](poller.md)。

## 抓包权限与 Npcap

Linux 原始套接字与 BSD BPF 设备需要相应权限或设备访问。
Windows 实时抓包需要可用的 Npcap 及其 DLL，打开失败时返回接口名与 Npcap 诊断。
单纯处理 pcap/pcapng 文件不需要抓包驱动。

## 测试与外部资源

带 `integration` tag 的测试可能需要权限或网卡，启用前阅读对应条件。
OTLP 测试使用本地 HTTP/gRPC 接收端并覆盖 TLS；Prometheus 测试抓取本地 handler，
均不依赖外部 Collector。模块检查方式见[开发说明](development.md)。

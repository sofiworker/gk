# gnet 存量子包审计报告

> 状态：2026-08-16 完成审计与 P0/P1 修复。四路并行审查 + 独立实测验证。
> 范围：addr/route/link/ethtool/netinfo、pcap/pcapng/packet、
> rawcap/capture/forward、layers。

## 已修复缺陷（14 项）

### P0（数据正确性/互操作性）

| 缺陷 | 位置 | 修复 |
|------|------|------|
| TPACKET_V3 包地址错位：`start` 漏加 `pktOffset`，V3 模式抓到的包内容全错位 | rawcap_linux.go | 补偏移；新增 VM 实测回归（TPACKET_V3 抓 lo 解码 2 包通过） |
| TPACKET_V3 无数据时 EAGAIN 忙等（100% CPU）+ ReadPacket 永不返回致 ctx 取消失效 | rawcap_linux.go | poll 等待（尊重 Timeout，超时返回 ErrReadTimeout） |
| pcapng BOM magic 语义反转（0x1A2B3C4D 标成小端），库内自闭环掩盖、与 Wireshark 等工具不互通 | pcapng/blocks.go | 交换常量语义；写读保持对称 |
| netinfo IPv6 解析整体 16 字节反转（正确为每 32 位字内小端），真实 /proc 数据验证 | netinfo/connections_linux.go | 按字反转；加真实 /proc/udp6 对照测试 |

### P1（功能性缺陷）

| 缺陷 | 位置 | 修复 |
|------|------|------|
| IPv4 序列化 IHL 硬编码 0x45，带选项时报文编码错位 | layers/encode.go | `(4<<4)\|ihl`；带选项编解码回环测试 |
| DNS 普通域名解析的 next 偏移恒为起点（question Type/Class 错位） | layers/dns.go | 按"首遇指针定格"语义修复 |
| pcapng 未知块（NRB/ISB/SPB 等）致 ReadPacket 中止 | pcapng/reader.go | 导出 `UnknownBlock` 原样返回跳过 |
| rawcap Close 与 readTPacket 的 mmap use-after-unmap 竞态 | rawcap_linux.go | 持锁提取包数据 + 循环内 closed 检查 |
| capture Timeout=0 时 ctx 取消无法中断阻塞读，Run 挂死 | capture/capture.go | ctx 路径关闭 handle（writer 留 Run 收尾），消除写路径竞态 |
| forward UDP 桥单向（remote 回包永不读回） | forward/udp.go | 每会话独立 upstream socket + 回包转发；双向并发测试通过 |
| forward TCP 超时无限重试，"超时"语义名不副实 | forward/tcp.go | 读超时 = 空闲超时，断开方向；文档更新 |
| Windows 下 addr(GUID) 与 link(友好名) 按名匹配恒失败 | netinfo/netinfo.go | 按 IfIndex 匹配，IfName 兜底 |
| route.List 的 IfName 字段永不填充 | route/route_linux.go | 一次 LinkList 建 index→name 映射填充 |
| forward 数据竞争（udp lastActive、tcp closed 直读） | forward | 全部经 mu 访问 |

### P2（顺手修复，含完整报告送达后的追加项）

- Windows addr：`fetchAdapters` 重试循环 + size=0 panic 防护 + Scope 按地址族粗略计算（原 `UnicastScope` 字段恒 0 已移除）；
- ethtool `parseSpeed` 未知哨兵兼容旧/新驱动（32 位全 0xffff 或仅低字段 0xffff 判未知，单侧高字段 0xffff 是合法速度）；
- netinfo 不再吞路由错误；route `List` 注释去 netlink 泄漏；spec 的 `ProtoCounters` 签名与实现（map）对齐；
- addr/route 双语注释补齐、addr 错误前缀统一；
- ethtool 内核结构体布局不变量测试（ifreqData 40B / ethtoolCmd 44B）；
- pcap 读包与 pcapng 读块分配上限（防恶意文件 OOM）、`packet.FromPCAP` OrigLen 回退、`FilterCopy` errors.Is、pcapng FilterCopy 注释诚实化、删除死代码 `GetTimestamp`；
- rawcap：block 边界检查、Stats 加锁、WritePacketData EBADF 映射；capture：writer Close 先 flush、pcapng 并发写互斥；forward：ByteOrderConfig 诚实标注 + 死字段清理。

- pcap/pcapng `NewFileWriter` 返回的 closer 绕过 `Writer.Close` 的 Flush（带缓冲时数据丢失）→ 改为 `w.Close`；
- pcap `FilterCopy` 不保时间戳精度 → 透传 `TimestampResolution`；
- layers 删除死代码错误、新增导出 `ErrTruncated` 哨兵（各层 too-short 错误可 `errors.Is`）；
- link 非 Linux 桩补导出 `ErrNotSupported`；
- TCP NS 标志序列化截断、TCP Options 与预设 DataOffset 冲突 → 修复；
- 新增实测：addr（18 地址）/route（29 路由）/link（16 网卡）/ethtool（e1000 1000M）/pcap·pcapng 畸形输入健壮性/TPACKET_V3/注入回捕/UDP 双向。

## 记录在案的设计缺口（未修，后续里程碑）

- layers：IPv6 扩展头链未处理、IPv6 jumbogram、解码侧不校验校验和；
- rawcap/capture：AF_PACKET(ETH_P_ALL)+SO_ATTACH_FILTER 在部分容器内核收不到帧（环境特性，tcpdump 用 protocol=0+TPACKET_V2 规避；测试已跳过并注明）；
- pcapng 多网卡并发写无互斥（单写者设计）；
- netinfo：ProtoCounters 非 Linux 缺失、Windows 后端未实测；
- ethtool：依赖废弃 GSET ioctl，无 ETHTOOL_GLINKSETTINGS 回退；
- 错误前缀不统一（部分 "rawcap:"/"capture:"，部分裸消息）——低风险批量清理留给后续。

## 验证

- 21 个包 `go test -race` 全绿；gofmt/vet/golangci-lint 干净；check-deps 通过；
- darwin/freebsd/windows 交叉编译通过；
- 覆盖率基线（审计前）：addr 10.9%、rawcap 6.8%、packet 20.7%、link 28.6%、ethtool 25.6%、route 33.3%——本轮均补实测/单测。

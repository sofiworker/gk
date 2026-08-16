# gk 模块依赖分层政策修订提案：gpoller 入基础契约层

> 状态：2026-08-16 提出，待 review。评审通过后合并进
> `2026-08-07-gk-module-dependency-policy.md` 第 2 节与第 3 节，
> 并同步 `scripts/check-deps.sh`。

## 背景

gnet 重构设计（见 `2026-08-16-gnet-network-foundation-redesign.md`）要求
自研事件引擎（epoll/kqueue/netpoller/io_uring 抽象 + 时间轮 + 缓冲原语）
同时作为 **gnet reactor** 的底层，并经 gnet 提供的标准库兼容组件
（epoll `net.Listener` + 引擎驱动的 `net.Conn`）供 **ghttp 等任何标准库
server** 由用户装配接入（2026-08-16 决策：ghttp 核心零改动、零 import）。

现行政策第 2 节把 `ghttp` 与 `gnet/*` 同列能力层，第 3.1 节禁止
「能力层 → 能力层」import。事件引擎的归属若放在 gnet 内，任何想复用
它的能力层都无法合法依赖；若各家各自实现，则违反第 3.4 节
「公共概念只实现一次」。两者矛盾，必须通过政策修订解决归属问题。
（注：ghttp 保持标准库底座、核心零感知；其与 gpoller 的关联仅通过
gnet 的标准库兼容组件 + 用户装配实现，不构成包级依赖。）

## 提案

### 1. 新增模块 `gpoller`，归入基础契约层

基础契约层新增「运行时原语组」，第 2 节表格修改为：

| 层 | 组 | 包含 | 依赖规则 |
|----|----|------|----------|
| 基础契约层 | 契约组 | `gerr`、`gretry`、`grx`、`gcompress`、`gcrypt` | 不依赖任何 `g*` 包；允许被上层引用 |
| 基础契约层 | 运行时原语组 | **`gpoller`** | 仅依赖标准库 + `golang.org/x/sys`；允许被上层引用 |

### 2. gpoller 的边界约束

- **只做运行时原语**：readiness 事件引擎（epoll/kqueue/netpoller）、
  completion 引擎（io_uring，旁路可选）、时间轮、零拷贝缓冲原语。
- **不含任何网络语义**：不定义 Server/Conn 生命周期、不做编解码、
  不 import 任何 `g*` 包（含 gerr/gretry——错误以普通 error + 平台错误码返回，
  由上层做 gerr 映射）。
- **purego 硬约束**：只经 `x/sys` / `syscall.Syscall6`，禁 cgo。

### 3. 政策第 3.4 节增补

「公共概念不重复」清单增补一行：

| 概念 | 权威定义 | 约束 |
|------|----------|------|
| 网络事件运行时原语（事件等待/定时/缓冲） | `gpoller` | gnet 通过依赖 gpoller 复用；其他能力层如需引擎须经 gnet 的标准库兼容组件装配，不得各自实现 epoll/kqueue 循环 |

### 4. check-deps.sh 同步

- `CAPABILITY_FAMILIES` 不变；
- 基础层白名单（如有）加入 `gpoller`；
- 新增检查：`gpoller` 不得 import 任何 `g*` 包（含基础契约层其他包），
  允许 stdlib + `x/sys`。

## 理由

1. 第 3.4 节「公共概念只实现一次」是政策自身的原则：事件引擎是
   gnet 与 ghttp 的共同地基，不抽走必然双份实现（已有前车之鉴：
   三套错误模型、三套重试实现）。
2. 调研佐证：Netty 把 transport 独立于 handler；tokio 把 runtime 内核
   与旁路分离；libp2p 把接口集中在 core、实现散子仓。运行时原语独立
   是主流做法。
3. ghttp API 已冻结（`2026-08-07-ghttp-api-freeze-v0.1.md`）且底座保持
   标准库；gpoller 独立成层后，gnet 提供标准库兼容的 epoll
   `net.Listener` + reactor `net.Conn`，ghttp 无需任何改动，由用户经
   现有 `Serve(ln net.Listener)` 选择接入。

## 评审清单

- [ ] 分层表修改是否接受「契约组 / 运行时原语组」拆分；
- [ ] gpoller 边界约束（无 g* 依赖、无网络语义）是否认可；
- [ ] 是否需要在 check-deps.sh 中把 gpoller 的依赖面机器化；
- [ ] 命名 `gpoller` 是否接受（备选：`gpoll`、`gevent`）。

## 迁移步骤（评审通过后）

1. 修改 `2026-08-07-gk-module-dependency-policy.md`（分层表 + 3.4 增补）；
2. 修改 `scripts/check-deps.sh`；
3. 按 `2026-08-16-gnet-network-foundation-redesign.md` 的 M1 开始实现 gpoller。

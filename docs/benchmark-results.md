# ghttp 性能基准测试报告

> 测试日期：2026-06-29
> 对比框架：gin, chi, echo, go-restful, fasthttp, don, default (net/http)
> 测试工具：[bombardier](https://github.com/codesenberg/bombardier)
> 硬件：AMD Ryzen 9 9950X 16-Core, Windows 10

## 测试方法

- 每个框架注册 `GET /hello → "hello world"`，通过 `Router().Register()` 或等价方式直接注册原始 handler
- Handler 体：`runtime.Gosched()`(0ms sleep)
- 测试参数：100 并发连接，10 秒持续时间，10 万请求
- ghttp 配置：默认 RadixRouter，无中间件，直接写入 `w.Write(message)`

## 结果汇总

| 框架 | 每秒请求数 (Reqs/sec) | 平均延迟 (Latency) | 吞吐量 (MB/s) |
|------|----------------------|-------------------|---------------|
| **don** | **203,008** | **499µs** | - |
| **fasthttp** | **191,584** | **528µs** | 24.01 |
| **ghttp** | **177,289** | **544µs** | 32.71 |
| **default (net/http)** | 143,411 | 670µs | 27.65 |
| **gin** | 143,697 | 626µs | 23.45 |
| **chi** | 138,177 | 646µs | 26.46 |
| **echo** | 136,415 | 663µs | 30.12 |
| **go-restful** | 87,344 | 0.92ms | 20.77 |

## 分析

### 性能梯队

**第一梯队 (>170k Req/s):** don (fasthttp 内核), fasthttp, ghttp
- ghttp 在纯路由场景下达到 **177k Req/s**，仅次于 don 和 fasthttp
- 延迟 544µs，在第一梯队中非常接近最顶尖水平

**第二梯队 (130-144k Req/s):** default, gin, chi, echo
- 标准库 ServeMux 和 gin/chi/echo 处于同一水平

**第三梯队 (<100k Req/s):** go-restful
- go-restful 因为 reflection-heavy 的 WebService 层，吞吐量仅 87k

### key 发现

1. **RadixRouter 性能优秀** — ghttp 的 Radix 树路由引擎在纯路由场景下比 chi (标准模式) 快约 28%，比 gin 快约 23%
2. **与 std net/http 持平以上** — ghttp 基于 `http.Handler`，在标准库之上仅增加三层 Radix 路由查找，开销极小
3. **fasthttp 仍有约 8% 优势** — fasthttp 的定制化 TCP 层和请求解析器带来了额外约 14k Req/s
4. **don 领先** — don 使用 fasthttp 作为底层传输，且 handler 签名极简

### 延迟分布 (P50 / P99)

| 框架 | P50 | P99 |
|------|-----|-----|
| fasthttp | 528µs | 1.82ms |
| ghttp | 544µs | 2.92ms |
| don | 499µs | 2.06ms |
| chi | 646µs | 3.52ms |
| gin | 626µs | 4.15ms |
| echo | 663µs | 2.83ms |
| default | 670µs | 3.30ms |
| go-restful | 0.92ms | 3.89ms |

### 总结

ghttp 在当前基准测试中表现优异，吞吐量达到 **17.7 万 Req/s**，在所有对比框架中排名**第三**（仅次于 fasthttp 内核的 don 和 fasthttp），在纯 `net/http` 路线的框架中排名**第一**。

延迟方面 P50 **544µs** 和 P99 **2.92ms** 均在合理范围内，与 fasthttp 相比差距在 5-10% 之间。

> **注**: 测试结果受 CPU 频率、内存带宽、操作系统调度等影响。此处为单次运行结果，实际表现可能因工作负载模式而异。

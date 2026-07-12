# ghttp Server Routing Rebuild Benchmark

日期：2026-07-12

## 测试边界

基准 harness 通过
`GHTTP_ROUTER_IMPL=new|legacy|legacy-radix|legacy-compiled|legacy-matchit|legacy-std`
选择新 `Server` 路由路径或冻结的历史路由器；`legacy` 是 `legacy-radix` 的兼容别名，
也是主基线。固定覆盖 16、128、1024、8192 条路由，以及 static、param、deep-param、
catch-all、HEAD explicit、HEAD GET fallback、method fallback、typed extraction、404 和
405。

两端共享最外层 `benchmarkRequestAdapter`、相同请求样本和同一个输入 response writer，
但内层 adapter 不同：新实现直接进入 `Server`，legacy 实现经
`benchmarkLegacyRequestAdapter` 安装 response state 和 request context 后再进入路由器。
因此结果用于发现请求路径回归，不能解释为严格的端到端业务处理成本。

尤其是 404、405 和 method fallback：新 `Server` 会执行框架 `ErrorHandler` 与 JSON
响应路径，而 legacy 只设置 `Allow`（405）并 `WriteHeader`。它们不能用于 matcher
的性能门槛判定。冻结基准的 `chains` 是每条 compiled route 加 400、404、405 三条
outcome chain。

## 环境与命令

- Go：go1.26.2 linux/amd64
- CPU：AMD Ryzen 7 8845HS with Radeon 780M Graphics
- OS/arch：Linux 7.0.14-201.fc44.x86_64 x86_64
- `benchstat`：`/root/go/bin/benchstat`。

请求路径原始样本使用以下命令，各有五轮：

```bash
GHTTP_ROUTER_IMPL=legacy go test ./ghttp -run '^$' -bench '^BenchmarkRouteComparison$' -benchmem -count=5 > /tmp/ghttp-route-legacy.txt
GHTTP_ROUTER_IMPL=new go test ./ghttp -run '^$' -bench '^BenchmarkRouteComparison$' -benchmem -count=5 > /tmp/ghttp-route-new.txt
```

安装 `benchstat` 后，为每侧补采一轮以满足其 95% 置信区间所需的最少六个样本，再执行比较：

```bash
GHTTP_ROUTER_IMPL=legacy go test ./ghttp -run '^$' -bench '^BenchmarkRouteComparison$' -benchmem -count=1 >> /tmp/ghttp-route-legacy.txt
GHTTP_ROUTER_IMPL=new go test ./ghttp -run '^$' -bench '^BenchmarkRouteComparison$' -benchmem -count=1 >> /tmp/ghttp-route-new.txt
benchstat /tmp/ghttp-route-legacy.txt /tmp/ghttp-route-new.txt
```

冻结基准的默认自适应校准会在每次 `b.N` 扩增时于计时外重建整台 Server；8192 路由下这会
花费大量墙钟时间而不增加任何冻结测量。因此已先中止该病态校准，改以固定一次冻结、五轮
采样记录结构成本：

```bash
go test ./ghttp -run '^$' -bench '^BenchmarkRouteFreeze$' -benchmem -benchtime=1x -count=5 > /tmp/ghttp-route-freeze-smoke.txt
```

以下表格保留最初五轮的算术均值，用于描述各场景的原始测量。后文的性能门槛结论以补采后的
六轮 `benchstat` 结果为准。

## 请求路径结果

| 路由数 / 场景 | legacy ns/op | legacy B/op / allocs | new ns/op | new B/op / allocs |
| --- | ---: | ---: | ---: | ---: |
| 16/404 | 619.5 | 800 / 6 | 1277.8 | 992 / 12 |
| 16/405 | 746.4 | 832 / 8 | 1452.8 | 1024 / 14 |
| 16/catch-all | 440.7 | 768 / 5 | 503.6 | 768 / 5 |
| 16/deep-param | 481.7 | 768 / 5 | 646.9 | 768 / 5 |
| 16/head-explicit | 438.5 | 784 / 6 | 494.1 | 768 / 5 |
| 16/head-get-fallback | 446.3 | 784 / 6 | 520.0 | 768 / 5 |
| 16/method-fallback | 707.8 | 832 / 8 | 1424.8 | 1024 / 14 |
| 16/param | 448.0 | 768 / 5 | 519.1 | 768 / 5 |
| 16/static | 420.5 | 768 / 5 | 487.8 | 768 / 5 |
| 16/typed-extraction | 603.3 | 864 / 6 | 841.0 | 864 / 6 |
| 128/404 | 624.7 | 800 / 6 | 1298.6 | 992 / 12 |
| 128/405 | 751.3 | 832 / 8 | 1488.2 | 1024 / 14 |
| 128/catch-all | 452.1 | 768 / 5 | 538.6 | 768 / 5 |
| 128/deep-param | 480.7 | 768 / 5 | 672.9 | 768 / 5 |
| 128/head-explicit | 440.8 | 784 / 6 | 519.8 | 768 / 5 |
| 128/head-get-fallback | 452.3 | 784 / 6 | 542.5 | 768 / 5 |
| 128/method-fallback | 701.6 | 832 / 8 | 1459.4 | 1024 / 14 |
| 128/param | 449.2 | 768 / 5 | 541.9 | 768 / 5 |
| 128/static | 429.1 | 768 / 5 | 522.4 | 768 / 5 |
| 128/typed-extraction | 621.0 | 864 / 6 | 852.6 | 864 / 6 |
| 1024/404 | 707.8 | 800 / 6 | 1365.2 | 992 / 12 |
| 1024/405 | 835.2 | 832 / 8 | 1575.2 | 1024 / 14 |
| 1024/catch-all | 503.9 | 768 / 5 | 543.7 | 768 / 5 |
| 1024/deep-param | 529.4 | 768 / 5 | 692.6 | 768 / 5 |
| 1024/head-explicit | 494.0 | 784 / 6 | 517.8 | 768 / 5 |
| 1024/head-get-fallback | 501.8 | 784 / 6 | 545.5 | 768 / 5 |
| 1024/method-fallback | 759.1 | 832 / 8 | 1538.8 | 1024 / 14 |
| 1024/param | 507.3 | 768 / 5 | 533.8 | 768 / 5 |
| 1024/static | 475.7 | 768 / 5 | 522.6 | 768 / 5 |
| 1024/typed-extraction | 676.8 | 864 / 6 | 883.1 | 864 / 6 |
| 8192/404 | 629.9 | 800 / 6 | 1120.2 | 992 / 12 |
| 8192/405 | 775.0 | 832 / 8 | 1310.4 | 1024 / 14 |
| 8192/catch-all | 419.1 | 768 / 5 | 393.1 | 768 / 5 |
| 8192/deep-param | 467.3 | 768 / 5 | 532.3 | 768 / 5 |
| 8192/head-explicit | 401.6 | 784 / 6 | 367.7 | 768 / 5 |
| 8192/head-get-fallback | 415.5 | 784 / 6 | 389.0 | 768 / 5 |
| 8192/method-fallback | 721.7 | 832 / 8 | 1289.8 | 1024 / 14 |
| 8192/param | 422.3 | 768 / 5 | 389.9 | 768 / 5 |
| 8192/static | 371.8 | 768 / 5 | 367.4 | 768 / 5 |
| 8192/typed-extraction | 604.9 | 864 / 6 | 700.1 | 864 / 6 |

成功匹配与 typed extraction 的 allocation 与 legacy 持平：一般成功路径为 5 次/768 B，
typed extraction 为 6 次/864 B。请求耗时仍不能据此宣称通过或未通过 5%--10% 门槛。

## 冻结结果

| 路由数 / middleware | ns/op | chains | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| 16/middleware=0 | 17390.4 | 19 | 22648 | 147.0 |
| 16/middleware=2 | 26629.0 | 19 | 22904 | 163.0 |
| 16/middleware=8 | 13553.2 | 19 | 23672 | 163.0 |
| 128/middleware=0 | 73953.4 | 131 | 165784 | 919.0 |
| 128/middleware=2 | 58691.2 | 131 | 167832 | 1047.0 |
| 128/middleware=8 | 63957.0 | 131 | 173976 | 1047.0 |
| 1024/middleware=0 | 1023153.2 | 1027 | 1319614 | 6940.2 |
| 1024/middleware=2 | 1205298.6 | 1027 | 1335998 | 7964.2 |
| 1024/middleware=8 | 1150178.4 | 1027 | 1385195 | 7964.6 |
| 8192/middleware=0 | 6720475.2 | 8195 | 10530894 | 54778.2 |
| 8192/middleware=2 | 7100464.4 | 8195 | 10661944 | 62970.0 |
| 8192/middleware=8 | 7335783.0 | 8195 | 11055205 | 62970.4 |

## 结论与限制

六轮 `benchstat` 显示成功路径存在显著的超过 10% 回归，因此本计划的性能门槛**未满足**，
后续性能优化应单独立项。代表性结果包括：16 路由 static `+15.91%`、param `+15.66%`、
deep-param `+33.48%`、typed extraction `+39.59%`；128 路由 static `+21.62%`、
deep-param `+39.57%`、typed extraction `+36.59%`；1024 路由 deep-param `+29.86%`、
typed extraction `+30.53%`；8192 路由 typed extraction `+15.74%`。这些行的 `p` 均小于
`0.05`。8192 路由 static 以及 1024 路由 HEAD explicit 没有显著差异，8192 路由的
param、catch-all 和 HEAD 路径则更快。

404、405 和 method fallback 不用于 matcher 门槛：新 `Server` 会运行完整 ErrorHandler
和 JSON 响应，而 legacy 只写最小状态。两端的内部 adapter 也不同，因此上述成功路径数据
用于报告整体请求路径回归，不能归因于 matcher 单一组件。没有为 404/405 引入 path-first
辅助索引；该选择保持 method-first 设计，并把额外错误响应成本清晰留在结果中。

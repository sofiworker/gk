# gsd

[English](README.en.md) | 中文

服务发现、注册、健康检查与负载均衡。

## 用法

```go
import "github.com/sofiworker/gk/gsd"
```

提供 etcd 注册/发现、健康检查（HTTP）与多种负载均衡策略（round-robin、随机、加权、最少连接），重试逻辑复用 `gretry`。

## 快照监听器（Snapshot Listeners）

监听器是可组合的装饰器（模式源自 linkerd2 的 destination listener），在向最终回调转发前过滤或合并服务快照：

- **`NewDedupListener(next)`** — 跳过与前一次相同地址集合的快照。watch 抖动/事件风暴会触发全量 Discover 并重复回调，本层过滤冗余通知。
- **`NewFallbackListener(next)`** — 合并主备两个发现源。主源有可用实例时用主源，主源为空时回退备源；两源都完成首次更新后才发布（避免备源未就绪时的过早空快照）。

```go
var instances []gsd.ServiceInfo

next := gsd.UpdateFunc(func(snapshot []gsd.ServiceInfo) {
    instances = snapshot
})

// 主/备 etcd 或 etcd + 本地缓存
primary, backup := gsd.NewFallbackListener(next)
dedup := gsd.NewDedupListener(primary)

registry.Watch("api", dedup.Update)         // 去重后 → 主源
fallbackRegistry.Watch("api", backup.Update) // 备源
```

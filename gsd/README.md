# gsd

[English](README.en.md) | 中文

服务发现、注册、健康检查与负载均衡。

## 用法

```go
import "github.com/sofiworker/gk/gsd"
```

提供 etcd 注册/发现、健康检查（HTTP）与多种负载均衡策略（round-robin、随机、加权、最少连接），重试逻辑复用 `gretry`。

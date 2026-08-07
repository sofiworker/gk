# gsd

服务发现、注册、健康检查与负载均衡。
Service discovery, registration, health checking and load balancing.

## 用法 / Usage

```go
import "github.com/sofiworker/gk/gsd"
```

提供 etcd 注册/发现、健康检查（HTTP）与多种负载均衡策略（round-robin、随机、加权、最少连接），重试逻辑复用 `gretry`。
Provides etcd registration/discovery, HTTP health checks and multiple load-balancing strategies (round-robin, random, weighted, least connections); retry logic reuses `gretry`.

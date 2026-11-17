# 使用说明

[English](../usage.md) | **简体中文** | [文档目录](README.md)

以下完整示例仅依赖根模块，支持 Go 1.25.12+。在已引入 Keel 的应用中，
分别保存为独立的 `main.go`，通过 `go run .` 运行。

## HTTP 处理函数

Server 实现 `net/http.Handler`。示例使用进程内 recorder，不开启监听端口：

```go
package main

import (
    "context"
    "fmt"
    stdhttp "net/http"
    "net/http/httptest"

    keelhttp "github.com/sofiworker/keel/http"
)

func main() {
    server := keelhttp.NewServer()
    route := keelhttp.Raw(stdhttp.MethodGet, "/hello",
        func(_ context.Context, _ *keelhttp.Request, response *keelhttp.Response) error {
            _, err := response.Write([]byte("hello"))
            return err
        })
    if err := server.Register(route); err != nil {
        panic(err)
    }
    recorder := httptest.NewRecorder()
    server.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, "/hello", nil))
    fmt.Println(recorder.Code, recorder.Body.String())
}
```

输出 `200 hello`。首次请求前完成路由和中间件注册；需要监听端口时，将 handler
交给标准库 HTTP 服务器，并由应用管理关闭。更多用法见[服务端](http.md)、
[客户端](http-client.md)与[遥测中间件](http-telemetry.md)。

## 内存缓存

```go
package main

import (
    "context"
    "fmt"
    "time"

    cache "github.com/sofiworker/keel/cache"
)

func main() {
    store, err := cache.NewMemoryCache(cache.WithCleanupInterval(0))
    if err != nil {
        panic(err)
    }
    defer store.Close()

    ctx := context.Background()
    if err := store.Set(ctx, "greeting", []byte("hello"), time.Minute); err != nil {
        panic(err)
    }
    value, err := store.Get(ctx, "greeting")
    if err != nil {
        panic(err)
    }
    fmt.Println(string(value))
}
```

输出 `hello`。显式别名 `cache` 可以与当前 `package gcache` 声明配合使用。
后端契约与本地缓存见[缓存说明](cache.md)。

## 组合与资源归属

通过各包接受的接口注入客户端、日志与遥测。数据库连接、HTTP Listener 和借用的 SDK
对象由应用管理；创建资源的一方负责关闭，托管遥测退出时使用新的、带超时的 context。

包文档中的示例也包含供应用集成的片段。完整可运行的 Agent 示例见
[Agent 示例](agent-examples.md)，目录名与包标识符的对应关系见[迁移指南](migration.md)。

# 配置

[English](../configuration.md) | **简体中文** | [文档目录](README.md)

[源码](../../config/) · `github.com/sofiworker/keel/config`

基于 Viper 的文件与环境变量配置加载。

## 用法

```go
import "github.com/sofiworker/keel/config"

loader, _ := config.New(config.WithFile("config.yaml"))
loader.Unmarshal(&cfg)

// 类型化访问器（懒加载）
host := loader.GetString("server.host")
port := loader.GetInt("server.port")
timeout := loader.GetDuration("server.timeout")
loader.UnmarshalKey("server", &serverCfg)
```

## 远程源

远程配置为可选；使用 Viper 远程 provider 需显式注册：

```go
import (
	_ "github.com/spf13/viper/remote"

	"github.com/sofiworker/keel/config"
)

loader, _ := config.New(
	config.WithRemoteProvider("etcd", "http://127.0.0.1:2379", "/config/app.yaml"),
)
```

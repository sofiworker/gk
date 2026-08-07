# gconfig

基于 Viper 的文件与环境变量配置加载。
Configuration loading from files and environment variables using Viper.

## 用法 / Usage

```go
import "github.com/sofiworker/gk/gconfig"

loader, _ := gconfig.New(gconfig.WithFile("config.yaml"))
loader.Unmarshal(&cfg)

// 类型化访问器（懒加载）/ typed accessors (lazy loading)
host := loader.GetString("server.host")
port := loader.GetInt("server.port")
timeout := loader.GetDuration("server.timeout")
loader.UnmarshalKey("server", &serverCfg)
```

## 远程源 / Remote Sources

远程配置为可选；使用 Viper 远程 provider 需显式注册：
Remote configuration is opt-in; Viper remote providers must be registered explicitly:

```go
import (
	_ "github.com/spf13/viper/remote"

	"github.com/sofiworker/gk/gconfig"
)

loader, _ := gconfig.New(
	gconfig.WithRemoteProvider("etcd", "http://127.0.0.1:2379", "/config/app.yaml"),
)
```

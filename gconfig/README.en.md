# gconfig

English | [中文](README.md)

Configuration loading from files and environment variables using Viper.

## Usage

```go
import "github.com/sofiworker/gk/gconfig"

loader, _ := gconfig.New(gconfig.WithFile("config.yaml"))
loader.Unmarshal(&cfg)

// typed accessors (lazy loading)
host := loader.GetString("server.host")
port := loader.GetInt("server.port")
timeout := loader.GetDuration("server.timeout")
loader.UnmarshalKey("server", &serverCfg)
```

## Remote Sources

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

# gconfig

Configuration loading from files, environment variables, and remote sources (using Viper).

## Usage

```go
import "github.com/sofiworker/gk/gconfig"

loader, _ := gconfig.New(gconfig.WithFile("config.yaml"))
loader.Unmarshal(&cfg)
```

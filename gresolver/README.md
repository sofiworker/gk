# gresolver

DNS Resolver configuration parsing and utilities.

## Usage

```go
import "github.com/sofiworker/gk/gresolver"

conf, _ := gresolver.ParseResolveFile("/etc/resolv.conf")
```

`ParseResolveFile` 完整解析 `nameserver`/`search`/`domain`/`options`
（含 `ndots:`/`timeout:`/`attempts:`），nameserver 自动补齐端口；缺省值可通过
`DefaultNameservers()` 获取副本。

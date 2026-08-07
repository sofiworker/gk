# gresolver

[English](README.en.md) | 中文

DNS 解析器配置解析与工具。

## 用法

```go
import "github.com/sofiworker/gk/gresolver"

conf, _ := gresolver.ParseResolveFile("/etc/resolv.conf")
```

`ParseResolveFile` 完整解析 `nameserver`/`search`/`domain`/`options`（含 `ndots:`/`timeout:`/`attempts:`），nameserver 自动补齐端口；缺省值可通过 `DefaultNameservers()` 获取副本。

`NewDefaultResolver`/`NewSystemResolver`/`NewPureGoResolver` 提供不同实现，`ResolverFactory` 支持按需选择。

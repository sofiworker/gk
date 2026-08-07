# gresolver

DNS 解析器配置解析与工具。
DNS resolver configuration parsing and utilities.

## 用法 / Usage

```go
import "github.com/sofiworker/gk/gresolver"

conf, _ := gresolver.ParseResolveFile("/etc/resolv.conf")
```

`ParseResolveFile` 完整解析 `nameserver`/`search`/`domain`/`options`（含 `ndots:`/`timeout:`/`attempts:`），nameserver 自动补齐端口；缺省值可通过 `DefaultNameservers()` 获取副本。
`ParseResolveFile` fully parses `nameserver`/`search`/`domain`/`options` (including `ndots:`/`timeout:`/`attempts:`); nameservers get default ports, and defaults are available via `DefaultNameservers()`.

`NewDefaultResolver`/`NewSystemResolver`/`NewPureGoResolver` 提供不同实现，`ResolverFactory` 支持按需选择。
Different implementations are available via `NewDefaultResolver`/`NewSystemResolver`/`NewPureGoResolver`, selectable through `ResolverFactory`.

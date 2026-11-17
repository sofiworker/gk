# DNS resolution

**English** | [简体中文](zh-CN/dns.md) | [Documentation](README.md)

[Source](../resolver/) · `github.com/sofiworker/keel/resolver`

DNS resolver configuration parsing and utilities.

## Usage

```go
import "github.com/sofiworker/keel/resolver"

conf, _ := resolver.ParseResolveFile("/etc/resolv.conf")
```

`ParseResolveFile` fully parses `nameserver`/`search`/`domain`/`options` (including `ndots:`/`timeout:`/`attempts:`); nameservers get default ports, and defaults are available via `DefaultNameservers()`.

Different implementations are available via `NewDefaultResolver`/`NewSystemResolver`/`NewPureGoResolver`, selectable through `ResolverFactory`.

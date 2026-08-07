# gresolver

English | [中文](README.md)

DNS resolver configuration parsing and utilities.

## Usage

```go
import "github.com/sofiworker/gk/gresolver"

conf, _ := gresolver.ParseResolveFile("/etc/resolv.conf")
```

`ParseResolveFile` fully parses `nameserver`/`search`/`domain`/`options` (including `ndots:`/`timeout:`/`attempts:`); nameservers get default ports, and defaults are available via `DefaultNameservers()`.

Different implementations are available via `NewDefaultResolver`/`NewSystemResolver`/`NewPureGoResolver`, selectable through `ResolverFactory`.

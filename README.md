# gk: Go Generic Kit

[![Go](https://github.com/sofiworker/gk/actions/workflows/go.yml/badge.svg)](https://github.com/sofiworker/gk/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/sofiworker/gk.svg)](https://pkg.go.dev/github.com/sofiworker/gk)
[![Go Report Card](https://goreportcard.com/badge/github.com/sofiworker/gk)](https://goreportcard.com/report/github.com/sofiworker/gk)

A comprehensive collection of Go libraries for building robust applications.

## Modules

- [gcache](gcache/README.md) - Caching (Memory, Redis, Valkey)
- [gcompress](gcompress/README.md) - Compression
- [gconfig](gconfig/README.md) - Configuration
- [gcrypt](gcrypt/README.md) - Cryptography
- [gerr](gerr/README.md) - Errors
- [ghttp](ghttp/README.md) - HTTP Client & Server
- [glog](glog/README.md) - Logging
- [gnet](gnet/README.md) - Networking & Packet Analysis
- [gotel](gotel/README.md) - OpenTelemetry
- [gresolver](gresolver/README.md) - DNS Resolver
- [gretry](gretry/README.md) - Retry Logic
- [grx](grx/README.md) - Reflection extensions
- [gsd](gsd/README.md) - Service Discovery & Load Balancing
- [gsql](gsql/README.md) - SQL Utilities

## Installation

```bash
go get github.com/sofiworker/gk
```

## Go Version

gk requires Go 1.25.0 or later.

Before v1.0.0, the minimum supported Go version is locked to Go 1.25.0.
All Go 1.25 patch releases are expected to compile and test the module.


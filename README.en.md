# gk: Go Generic Kit

> **⚠️ UNDER DEVELOPMENT — DO NOT USE IN PRODUCTION.**
> This repository is actively developed; APIs are unstable and unverified for production use.

[![Go](https://github.com/sofiworker/gk/actions/workflows/go.yml/badge.svg)](https://github.com/sofiworker/gk/actions/workflows/go.yml)
[![golangci-lint](https://github.com/sofiworker/gk/actions/workflows/go.yml/badge.svg)](https://golangci-lint.run/)
[![codecov](https://codecov.io/gh/sofiworker/gk/graph/badge.svg)](https://app.codecov.io/gh/sofiworker/gk)
[![Go Reference](https://pkg.go.dev/badge/github.com/sofiworker/gk.svg)](https://pkg.go.dev/github.com/sofiworker/gk)

English | [中文](README.md)

A collection of Go libraries for building robust applications; packages are independent and composable.

## Modules

- [gcache](gcache/README.en.md) - Caching (in-process implementation + injected backend contracts, dependency-free)
- [gcompress](gcompress/README.en.md) - Compression
- [gconfig](gconfig/README.en.md) - Configuration
- [gcrypt](gcrypt/README.en.md) - Cryptography
- [gerr](gerr/README.en.md) - Errors
- [ghttp](ghttp/README.en.md) - HTTP Client & Server
- [glog](glog/README.en.md) - Logging
- [gnet](gnet/README.en.md) - Networking foundation: reactor server, capture, packet analysis & forwarding
- [gotel](gotel/README.en.md) - Observability abstractions
- [gresolver](gresolver/README.en.md) - DNS Resolver
- [gretry](gretry/README.en.md) - Retry Logic
- [grx](grx/README.en.md) - Reflection extensions
- [gsd](gsd/README.en.md) - Service Discovery & Load Balancing
- [gsql](gsql/README.en.md) - SQL Utilities

## Installation

```bash
go get github.com/sofiworker/gk
```

## Go Version

gk requires Go 1.25.0 or later. Before v1.0.0 the minimum is locked to Go 1.25.0; all Go 1.25 patch releases are expected to compile and test the module.

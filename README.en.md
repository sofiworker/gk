# Keel: Go Libraries

> **⚠️ UNDER DEVELOPMENT — DO NOT USE IN PRODUCTION.**
> This repository is actively developed; APIs are unstable and unverified for production use.

[![CI](https://github.com/sofiworker/keel/actions/workflows/go.yml/badge.svg?branch=master&event=push)](https://github.com/sofiworker/keel/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/sofiworker/keel/branch/master/graph/badge.svg)](https://app.codecov.io/gh/sofiworker/keel)
[![Go Reference](https://pkg.go.dev/badge/github.com/sofiworker/keel.svg)](https://pkg.go.dev/github.com/sofiworker/keel)

English | [中文](README.md)

Keel takes its name from the backbone of a ship. It is a collection of Go libraries for building robust applications; packages are independent and composable.

## Modules

- [gai](gai/README.en.md) - Agent SDK core definitions and UUID v7 utilities (runtime not yet implemented)
- [gcache](gcache/README.en.md) - Caching (in-process implementation + injected backend contracts, dependency-free)
- [gcompress](gcompress/README.en.md) - Compression
- [gconfig](gconfig/README.en.md) - Configuration
- [gcrypt](gcrypt/README.en.md) - Cryptography
- [gerr](gerr/README.en.md) - Errors
- [ghttp](ghttp/README.en.md) - HTTP Client & Server
- [glog](glog/README.en.md) - Logging
- [gnet](gnet/README.en.md) - Networking foundation: reactor server, capture, packet analysis & forwarding
- [gotel](gotel/README.en.md) - Vendor-neutral tracing and metrics with opt-in backend modules
- [gresolver](gresolver/README.en.md) - DNS Resolver
- [gretry](gretry/README.en.md) - Retry Logic
- [grx](grx/README.en.md) - Reflection extensions
- [gsd](gsd/README.en.md) - Service Discovery & Load Balancing
- [gsql](gsql/README.en.md) - SQL Utilities

## Installation

The root module requires Go 1.25.12+. The optional `gotel/adapters/otel`, `gotel/adapters/otlp` and `gotel/adapters/otelprometheus` modules require Go 1.26.0+ for their OpenTelemetry dependencies.

```bash
go get github.com/sofiworker/keel
```

## Migrating from gk (breaking change)

The project is now named Keel. Replace the `github.com/sofiworker/gk` prefix with `github.com/sofiworker/keel` in Go imports and `go.mod` (including `require` and `replace`), then run `go mod tidy` and your project's tests. Update all four standalone `gotel/adapters/*` module paths as well. Subpackage names such as `ghttp` and `gnet` are unchanged.

For example: `import "github.com/sofiworker/keel/ghttp"`.

The OpenTelemetry adapter's default instrumentation scope name is now `github.com/sofiworker/keel/gotel`. Update dashboards and queries that depend on the old scope name, or explicitly configure the previous name with `WithInstrumentationName`.

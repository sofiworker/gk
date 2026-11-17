# Keel

**English** | [简体中文](README.zh-CN.md)

Composable Go libraries for HTTP, networking, caching, configuration, telemetry and agents.
A keel is the backbone of a ship; each package supplies a part of an application's foundation.

[![CI](https://github.com/sofiworker/keel/actions/workflows/go.yml/badge.svg?branch=master&event=push)](https://github.com/sofiworker/keel/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/sofiworker/keel/branch/master/graph/badge.svg)](https://app.codecov.io/gh/sofiworker/keel)
[![Go Reference](https://pkg.go.dev/badge/github.com/sofiworker/keel.svg)](https://pkg.go.dev/github.com/sofiworker/keel)

> **Under development, pre-v1.0.0. Do not use directly in production.**
> APIs and storage formats may change without compatibility guarantees.
> See [development status and usage restrictions](docs/development.md#development-status).

## Capabilities

| Area | Content | Documentation |
| --- | --- | --- |
| HTTP | Server, client, shared wire format, tracing and metrics middleware | [HTTP server](docs/http.md), [client](docs/http-client.md) |
| Networking | Reactor, capture, pcap/pcapng, framing, multiplexing and forwarding | [Networking](docs/networking.md), [platform support](docs/platform-support.md) |
| Utilities | Cache, configuration, compression, cryptography, errors, logging, retries and reflection | [Package guides](docs/README.md#package-guides) |
| Infrastructure | DNS, service discovery, load balancing and SQL | [DNS](docs/dns.md), [discovery](docs/discovery.md), [SQL](docs/sql.md) |
| Telemetry | Backend-neutral tracing and metrics with four optional backend modules | [Telemetry](docs/telemetry.md) |
| Agents | Model/tool execution, sessions, context budgets and in-memory sandboxes | [Agent SDK](docs/agent.md) |

## Installation

The root module requires **Go 1.25.12+**. The OTel, OTLP and OTel Prometheus adapter
modules require **Go 1.26.0+**. Build-tagged HTTP generic methods require Go 1.27+.

```sh
go get github.com/sofiworker/keel
```

Optional backends currently use local replacements for development.
See [Installation](docs/installation.md) before using them in another project.

## Quick start

Import the package you need. Explicit aliases distinguish it from standard library packages:

```go
import (
    stdhttp "net/http"

    keelhttp "github.com/sofiworker/keel/http"
    cache "github.com/sofiworker/keel/cache"
)
```

[Usage](docs/usage.md) contains complete runnable HTTP and in-memory cache examples.

## Documentation

| Document | Content |
| --- | --- |
| [Documentation index](docs/README.md) | All guides and package documentation |
| [Installation](docs/installation.md) | Toolchains, root module and optional backends |
| [Usage](docs/usage.md) | Complete examples and lifecycle conventions |
| [Architecture](docs/architecture.md) | Package responsibilities and dependency boundaries |
| [Migration](docs/migration.md) | Moving from gk and g-prefixed import paths |
| [Platform support](docs/platform-support.md) | Network backends, privileges and Windows Npcap |
| [Development](docs/development.md) | Repository layout, status, checks and document conventions |
| [Release process](docs/release.md) | Versioning and publishing the five modules |
| [Changelog](CHANGELOG.md) | Recorded breaking changes |

Chinese documentation is in [docs/zh-CN](docs/zh-CN/README.md).

## Contributing

Report bugs through [GitHub issues](https://github.com/sofiworker/keel/issues) and submit
changes through pull requests. Include the package, Go version, OS and a minimal reproducer.
Follow the checks described in [Development](docs/development.md).


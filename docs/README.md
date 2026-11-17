# Keel documentation

**English** | [简体中文](zh-CN/README.md)

Documentation for the current development version. See the [project overview](../README.md) and [development status](development.md#development-status).

## Getting started

| Guide | Content |
| --- | --- |
| [Installation](installation.md) | Toolchains and module setup |
| [Usage](usage.md) | Runnable examples and resource ownership |
| [Architecture](architecture.md) | Responsibilities and dependency boundaries |
| [Migration](migration.md) | Repository/package rename and identifiers |
| [Platform support](platform-support.md) | Network backends and prerequisites |

## Package guides

| Package directory | Guide |
| --- | --- |
| [`agent`](agent.md) | Agent definitions and execution |
| [`cache`](cache.md) | Contracts, memory cache, LRU/LFU/TTL |
| [`compress`](compress.md) | Gzip, zlib, flate and archives |
| [`config`](configuration.md) | File, environment and remote configuration |
| [`crypt`](crypt.md) | Encryption and signing |
| [`gerr`](errors.md) | Error trees, kinds and codes |
| [`http`](http.md) | Routing, binding, middleware and OpenAPI |
| [`log`](logging.md) | Structured logs and rotation |
| [`net`](networking.md) | Reactor, capture, packets and forwarding |
| [`otel`](telemetry.md) | Portable tracer and meter contracts |
| [`poller`](poller.md) | Event engines, ring buffer and timing wheel |
| [`resolver`](dns.md) | DNS configuration and implementations |
| [`retry`](retry.md) | Backoff, jitter and cancellation |
| [`rx`](reflection.md) | Reflection helpers |
| [`sd`](discovery.md) | Discovery, registration and balancing |
| [`sql`](sql.md) | Queries, transactions and migrations |

## HTTP and telemetry

- [HTTP client](http-client.md)
- [HTTP wire format](http-wire.md)
- [HTTP telemetry](http-telemetry.md)
- [OpenTelemetry adapter](otel-adapter.md)
- [OTLP export](otlp.md)
- [OTel Prometheus export](otel-prometheus.md)
- [Native Prometheus metrics](prometheus.md)

## Agent topics

- [Agent runner](agent-runner.md)
- [Model context](agent-context.md)
- [UUID v7](agent-id.md)
- [Model contracts](agent-model.md)
- [Agent runtime](agent-runtime.md)
- [Sandbox](agent-sandbox.md)
- [Session storage](agent-session.md)
- [Tools](agent-tools.md)
- [Built-in file tools](agent-builtin-tools.md)
- [Model HTTP adapter](agent-model-http.md)
- [Agent HTTP transport](agent-http-transport.md)
- [Agent examples](agent-examples.md)

## Development and maintenance

- [Development](development.md): status, source layout, checks and document conventions.
- [Release process](release.md): versions, module tags and publication order.
- [Changelog](../CHANGELOG.md): breaking changes.
- [Coding conventions](../AGENTS.md): repository rules.

## Reading conventions

Topic titles and directory names may differ from Go package declarations; see
[Migration](migration.md#package-identifiers). Fragments run in application context;
complete programs are in [Usage](usage.md). English topics live in docs/ and Chinese
translations use matching filenames in docs/zh-CN/.

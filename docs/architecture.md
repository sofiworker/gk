# Architecture

**English** | [简体中文](zh-CN/architecture.md) | [Documentation](README.md)

## Package boundaries

Keel has one root Go module and four independent telemetry backend modules.
Applications import individual packages from the root module and explicitly compose runtimes and telemetry backends.
Each package's imports determine its dependencies.

| Layer | Responsibility | Guides |
| --- | --- | --- |
| Utilities | Cache contracts, errors, configuration, logging, retries, cryptography and compression | [Package index](README.md#package-guides) |
| Network runtime | Readiness/completion contracts, ring buffer and timing wheel | [Poller](poller.md) |
| Network capabilities | Reactor, capture, decoding, framing, multiplexing and forwarding | [Networking](networking.md) |
| HTTP | Server/client policy and shared protocol primitives | [Server](http.md), [client](http-client.md), [wire](http-wire.md) |
| Telemetry contracts | Tracer, meter, instruments and lifecycle interfaces | [Telemetry](telemetry.md) |
| Integration adapters | HTTP telemetry and vendor SDK integration | [HTTP telemetry](http-telemetry.md), [OTel](otel-adapter.md) |
| Agent execution | Models, tools, sessions, contexts and sandboxes | [Agent SDK](agent.md) |

## HTTP and telemetry

```text
http server ──→ http/wire ←── http/client
http/adapters/gotel ──→ http + otel core
otel/adapters/* ──→ otel core + vendor SDK
```

The wire layer provides strict decoding, SSE parsing and status-code contracts.
Retry, routing and response policy stay with each endpoint. The HTTP telemetry adapter
translates portable tracer/meter interfaces into middleware.

The root module does not require optional backend modules. Each backend has its own
`go.mod` and checks. Build tags select platform/toolchain implementations;
module boundaries isolate vendor dependencies.

## Agent execution

An Agent defines behavior, a Session selects the model and environment, and a Turn records
execution. Runtime commits model/tool intent before external operations and results through
a Store. Sandbox views expose capabilities scoped to each call.

The built-in store and sandbox are in memory; they do not provide crash recovery or OS
process isolation. Host file synchronization is explicit and separate from session commits.
See [Runtime](agent-runtime.md), [storage](agent-session.md) and [Sandbox](agent-sandbox.md).

## Platform boundaries

Readiness backends use Linux epoll or BSD/macOS kqueue; raw capture uses platform-specific
implementations. Unsupported capabilities return errors. See [Platform support](platform-support.md).

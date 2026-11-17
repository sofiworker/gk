# Installation

**English** | [简体中文](zh-CN/installation.md) | [Documentation](README.md)

## Toolchains

| Module or feature | Minimum Go version |
| --- | --- |
| Root module, including HTTP and telemetry core | 1.25.12 |
| Native Prometheus adapter | 1.25.12 |
| Base OTel, OTLP and OTel Prometheus adapters | 1.26.0 |
| Build-tagged HTTP generic methods | 1.27 |

OpenTelemetry v1.47 determines the higher minimum for the three OTel SDK modules.
CI also checks Go 1.27.1. Older supported versions use HTTP function APIs instead of
the build-tagged generic methods.

## Root module

```sh
go get github.com/sofiworker/keel
```

Import individual packages, such as `github.com/sofiworker/keel/http/client`.
During the rename, some folder names differ from their declared Go package names;
[Migration](migration.md#package-identifiers) lists the current differences.

## Optional telemetry backends

The root module's telemetry core does not import vendor SDKs. Each backend has a separate module:

| Import path after `github.com/sofiworker/keel/` | Use |
| --- | --- |
| `otel/adapters/otel` | Borrow existing OTel APIs or own an SDK provider |
| `otel/adapters/otlp` | Export traces and metrics over OTLP |
| `otel/adapters/otelprometheus` | Own an OTel provider and expose a Prometheus handler |
| `otel/adapters/prometheus` | Implement metrics directly with Prometheus |

These modules currently require siblings at `v0.0.0` and use local replacements.
They are development modules, not independently installable at those versions.
Consumers do not inherit a dependency's replacements.

Clone the complete repository for local development. A separate application must replace
every Keel module it imports with the appropriate checkout directory. For example,
an OTLP application uses these directives in its own `go.mod`:

```gomod
replace github.com/sofiworker/keel => ../keel
replace github.com/sofiworker/keel/otel/adapters/otel => ../keel/otel/adapters/otel
replace github.com/sofiworker/keel/otel/adapters/otlp => ../keel/otel/adapters/otlp
```

Adjust the paths and run `go mod tidy`. Add replacements for the Prometheus backend
you choose. Published versions must be pinned before removing replacements; see [Release process](release.md).

## Source checkout

```sh
git clone git@github.com:sofiworker/keel.git
cd keel
go mod download
go test ./...
```

The final command covers the root module. Run adapter checks in their own directories
as described in [Development](development.md). No global `go.work` is required.
Network backend availability depends on the [platform](platform-support.md).

# Development

**English** | [简体中文](zh-CN/development.md) | [Documentation](README.md)

## Development status

**The entire repository is under development (pre-v1.0.0). Do not use directly in production.**
This repository-level status takes precedence over individual feature descriptions.

APIs may be removed, renamed or changed without compatibility guarantees. Behavior and
documentation are evolving and have not been validated for production.
HTTP generic methods have Go-version-specific implementations behind build tags.

Use is limited to internal experiments, prototypes, learning and development integration.
If another use is necessary, first confirm the version and stability with the maintainer
or wait for v1.0.0. Publishing or deploying an application based on this repository is not
authorized merely by the feature documentation.

Breaking changes must be explicit in documentation and commit messages.
Pushing, tagging, releasing and deployment require maintainer authorization.
Documented defaults describe the current implementation, not a frozen contract.

## Repository layout

```text
agent/                     Model/tool execution, sessions, context and sandbox
cache/, config/, log/       Application utilities
compress/, crypt/, gerr/     Data and error helpers
http/                       Server, client, wire format and HTTP telemetry
net/, poller/, resolver/     Networking capabilities and runtime primitives
otel/                       Backend-neutral telemetry contracts
otel/adapters/*/            Four independent backend modules
retry/, rx/, sd/, sql/       Retry, reflection, discovery and database utilities
docs/                       English topic guides
docs/zh-CN/                 Chinese guides with matching filenames
.github/workflows/go.yml    Current CI checks
```

See [Architecture](architecture.md) for responsibility and dependency boundaries and
[AGENTS.md](../AGENTS.md) for repository coding conventions.

## Checks

The root module and all four backend modules are independent check targets:

```sh
make check
go test ./...
```

Run the following from the repository root in a POSIX shell:

```sh
for adapter in otel prometheus otlp otelprometheus; do
    (
        cd "otel/adapters/$adapter" &&
        go mod tidy -diff &&
        go vet ./... &&
        go test ./...
    ) || exit 1
done
```

On PowerShell, enter each corresponding directory and run the same three Go commands.
Root tests do not enter nested modules. CI also runs lint, Go-version checks, formatting,
race detection and coverage. Use Go 1.27+ gofmt for build-tagged generic method syntax.

```sh
golangci-lint run ./...
go test -race ./...
```

Run lint in each backend module as well. For dependency analysis, use
`go mod tidy -diff` in the root and each backend.
Integration tests can require privileges or external resources; see [Platform support](platform-support.md).

`make check-adapters` runs formatting, vet and tests in all four backend modules.
CI uses the same `otel/adapters/*` source directories.

## Documentation conventions

- Root README.md is the English project introduction; README.zh-CN.md is the Chinese entry.
- Detailed guides live in docs/; translations use matching filenames in docs/zh-CN/.
- Every topic links to its translation and the documentation index.
- Package code stays in its source directory; detailed guides are not duplicated beside it.
- When moving a document, update relative source/license links and anchors.
- Describe current implementations and distinguish import paths from package identifiers.
- Include new topics in both indexes. Keep examples and version requirements aligned.

## Changes and releases

Use Conventional Commits and keep changes scoped. Document compatibility changes in the
[changelog](../CHANGELOG.md) and [migration guide](migration.md). Follow the
[release process](release.md); do not infer authorization to publish from local validation.

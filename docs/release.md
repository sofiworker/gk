# Release process

**English** | [简体中文](zh-CN/release.md) | [Documentation](README.md)

Keel remains pre-v1.0.0, under development and prohibited for direct production use.
See [Development status](development.md#development-status).

## Versioning

Use SemVer. During v0.x, breaking changes bump the minor version; compatible additions
and fixes bump the patch version. Record every breaking change in [CHANGELOG.md](../CHANGELOG.md)
and update the affected guides and [migration instructions](migration.md).

## Checks

Validate the minimum supported Go version of each module and the supported newer toolchains:

```sh
make check
go test ./...
golangci-lint run ./...
go test -race ./...
```

Use Go 1.27.1 to format/check the build-tagged generic method files.
Run tidy, vet, test and lint separately in all four backend module directories.
See [Development](development.md#checks) for the exact directory list and aggregate target.
Confirm API examples, defaults and version requirements against the current source.

## Module order

Backend v0.0.0 requirements and local replace directives are development placeholders.
Publish and pin actual versions in this order:

| Order | Module | Tag |
| --- | --- | --- |
| 1 | Root | vX.Y.Z |
| 2 | Base OTel adapter | otel/adapters/otel/vX.Y.Z |
| 2 | Native Prometheus adapter | otel/adapters/prometheus/vX.Y.Z |
| 3 | OTLP adapter | otel/adapters/otlp/vX.Y.Z |
| 3 | OTel Prometheus adapter | otel/adapters/otelprometheus/vX.Y.Z |

The base OTel/native Prometheus modules depend on the root; OTLP/OTel Prometheus also
depend on the base adapter. Consumers do not inherit local replacements.
Check installation from a separate consumer module using the intended published versions.

## Publishing

1. Turn the Unreleased changelog into a dated version entry, including breaking changes.
2. Update affected guides and migration notes; verify documentation links.
3. Pass local checks and the remote CI jobs for the release commit.
4. With maintainer authorization, create and push each required module tag.
5. Create a GitHub Release describing the changes and development-only status.
6. Update README version and Go requirements when they change.

Tagging, publishing a release and deploying are separate operations requiring authorization.
Local edits, tests or an existing feature description do not authorize them.

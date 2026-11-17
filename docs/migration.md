# Migration

**English** | [简体中文](zh-CN/migration.md) | [Documentation](README.md)

## Repository and import paths

The repository is now `github.com/sofiworker/keel`. Update the module prefix
from `github.com/sofiworker/gk`, then update the first package path segment:

| Previous | Current | Previous | Current |
| --- | --- | --- | --- |
| `gai` | `agent` | `gcache` | `cache` |
| `gcompress` | `compress` | `gconfig` | `config` |
| `gcrypt` | `crypt` | `ghttp` | `http` |
| `glog` | `log` | `gnet` | `net` |
| `gotel` | `otel` | `gpoller` | `poller` |
| `gresolver` | `resolver` | `gretry` | `retry` |
| `grx` | `rx` | `gsd` | `sd` |
| `gsql` | `sql` | `gerr` | `gerr` (unchanged) |

Preserve the remaining path segments. For example, the HTTP client is
`github.com/sofiworker/keel/http/client`. The HTTP telemetry adapter still lives
at `http/adapters/gotel`; the backend modules now live at `otel/adapters/*`.

Update imports, application `require`/`replace` directives, scripts and CI working
directories, then run `go mod tidy` and application tests. The four backend modules
must also be checked separately.

## Package identifiers

Several folders currently retain their previous package declarations. Go takes the default
import identifier from the declaration, not from the last directory in the path:

| Directory | Declared package | Suggested explicit alias |
| --- | --- | --- |
| `cache` | `gcache` | `cache` |
| `http` | `ghttp` | `keelhttp` |
| `log` | `glog` | `keellog` |
| `net` | `gnet` | `keelnet` |
| `otel` | `gotel` | `telemetry` |
| `retry` | `gretry` | `retry` |
| `http/adapters/gotel` | `ghttpotel` | `httpotel` |

Existing detailed guides may use these declared identifiers. Explicit aliases make code work
with the current declarations and avoid collisions with standard or vendor packages:

```go
import (
    stdhttp "net/http"
    keelhttp "github.com/sofiworker/keel/http"
)
```

## Instrumentation scope

The managed OTel adapter currently uses `github.com/sofiworker/keel/gotel`
as its default scope name. The folder rename does not change that runtime string.
Use `WithInstrumentationName` to set an explicit name when dashboards or queries depend on it.

## Documentation paths

Root `README.md` is English; Chinese is `README.zh-CN.md`.
Detailed English guides live under `docs/`, with matching filenames under
`docs/zh-CN/`. Package-local READMEs and the former `README.en.md` files
have been consolidated into this documentation tree. Start at the [index](README.md).

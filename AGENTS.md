Always respond in Chinese-simplified

# Repository Guidelines

## Project Structure & Module Organization
The repository is a single Go module (`github.com/sofiworker/gk`) composed of focused packages under the top level. Core services live in `ghttp/` (HTTP client, server, codecs), network utilities in `gnet/`, caching helpers in `gcache/`, compression support in `gcompress/`, and data access tools in `gsql/`. Supporting libraries such as logging (`glog/`), crypto (`gcrypt/`), telemetry (`gotel/`), and reactive helpers (`grx/`) sit alongside these modules. Example programs reside in `example/`, which is useful for quick manual checks. Tests accompany their packages as `_test.go` files within each directory—for example, `ghttp/gserver/server_test.go`.

## Build, Test, and Development Commands
Use `go build ./...` from the repo root to confirm every package compiles with the current Go toolchain. Run `go test ./...` before submitting changes to exercise all unit tests; focus on individual areas with commands such as `go test ./ghttp/gclient -run TestRequest`. Static checks with `go vet ./...` catch common misuses and should be included in pre-PR runs. When editing generated assets, ensure the generator (if present inside `example/` or package directories) is documented in your change description.

## Coding Style & Naming Conventions
Format code with `gofmt`; integrate it into your editor or run `go fmt ./...` on touched packages. Follow idiomatic Go naming: packages use short, lowercase names; exported identifiers use PascalCase and include domain context (e.g., `NewResolver`, `HTTPClient`). Keep files focused by concern—protocol codecs in `ghttp/codec`, shared types in `internal` subpackages when privacy is required, and avoid circular dependencies between sibling packages.

## Testing Guidelines
Tests use Go’s standard `testing` package. Mirror the package structure so every feature has a co-located `_test.go` file, and name tests `TestFeatureScenario` for clarity. Add table-driven cases for edge conditions—particularly in concurrent networking code (`gnet/`) and request routing (`ghttp/gserver`). Run `go test -race ./...` when touching code that manages goroutines or shared state. Aim to maintain or raise coverage for packages you modify, documenting any intentionally untested paths (e.g., OS-specific behavior).

## Commit & Pull Request Guidelines
Commits follow a conventional prefix format (`feat:`, `fix:`, `refactor:`) paired with a concise summary, as seen in recent history. Keep each commit focused and include context in the body when behavior or configuration changes. Pull requests should link to tracking issues when available, enumerate functional changes, list test commands executed, and add screenshots or logs for observable UI/API shifts. Request reviews from maintainers responsible for the touched package and ensure all CI jobs pass before merging.

## Tool Priority
- 文件名搜索：使用 `find`
- 文本内容搜索：使用 `grep`
- 代码结构搜索：使用 `ast-grep`
- 排除目录：`.git`, `node_modules`, `dist`, `coverage`
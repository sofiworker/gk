Always respond in Chinese-simplified

# 仓库状态

- 整个仓库处于开发中（pre-v1.0.0），**禁止直接用于生产开发**；详情见根目录 `DEVELOPMENT.md`。
- 所有改动默认不承诺向后兼容；破坏性变更必须在提交说明与 README 中显式标注。
- 未经维护者明确授权，不得向远程推送、打 tag、发布或部署。
- 模块依赖遵循三层分层原则：基础契约层（gerr/gretry/grx 等）可被上层引用；能力层（ghttp/glog/gsd 等）之间禁止互相 import；拼接走接口 + 适配层。详见 `docs/superpowers/specs/2026-08-07-gk-module-dependency-policy.md`。

# Repository Guidelines

## Code Style (Go)

- Formatting: Format code with `gofmt`; integrate it into your editor or run `go fmt ./...` on touched packages.
- Naming: Use `camelCase` for variables, `PascalCase` for types, and `snake_case` for file names.
- Linting: fixes must satisfy `golangci-lint` (same config used in CI).
- Errors: wrap with context; return early; avoid panics in library code.
- Concurrency: use contexts where appropriate; avoid data races; prefer channels or mutexes over ad‑hoc globals.
- Logging: keep logs concise, actionable, and consistent with existing patterns.
- Public API/flags: changing behavior or flags requires README updates and tests.
- Context: First parameter for blocking operations
- Use `go mod tidy` to check or add dependencies
- Function definitions should be relatively independent to facilitate unit testing 
- Each function needs to have corresponding unit tests written 
- Prefer system calls over writing command executions 
- Implementations for different platforms rely on Golang build tags
- Expose constants or errors as much as possible to facilitate user-side judgment
- Prefer defining small interfaces and then composing them, rather than directly defining complete interfaces.
- Most configurations in config can be set via WithFunc.

## Tests

- Location: alongside code as `*_test.go`.
- Scope: unit tests near behavior changes; table‑driven where it fits.
- Unless explicitly required, only execute unit tests related to the changes, rather than all unit tests.
- Run locally: `go test ./...`

## PR & Commit Guidelines

- Conventional Commits (`feat:`, `fix:`, `chore:` …)
- Include rationale and tradeoffs in the PR description.
- Link related issues; note breaking changes clearly.

## For AI Coding Agents

- Planning: for multi‑step tasks, maintain an explicit plan and mark progress.
- Edits: use a single focused patch per logical change; prefer `apply_patch`.
- Preambles: briefly state what you’re about to do before running commands.
- Searches: prefer `rg` and read files in ≤250‑line chunks.
- Validation: run `make check` and `go test ./...` to verify changes.
- Scope control: avoid touching unrelated files; don’t reformat the repo wholesale.
- No external network: assume offline; do not add dependencies lightly.

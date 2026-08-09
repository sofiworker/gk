# k6 Resource Sampler Relocation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将资源采样器完全迁入 `ghttp/testdata/k6` 独立模块，删除对仓库根模块的测试辅助包污染。

**Architecture:** `cmd/resources` 与 `internal/resources` 作为 k6 测试模块的内部工具存在。PowerShell 与 Unix runner 只在 k6 module 中构建三个测试二进制，不再切换到仓库根目录。

**Tech Stack:** Go 1.26、build tags、PowerShell、POSIX shell。

---

### Task 1: 迁移资源采样器

**Files:**
- Move: `cmd/resources/*` → `ghttp/testdata/k6/cmd/resources/*`
- Move: `internal/resources/*` → `ghttp/testdata/k6/internal/resources/*`

- [x] 复制实现和测试到 k6 module。
- [x] 将 import 改为 `github.com/sofiworker/gk/ghttp/testdata/k6/internal/resources`。
- [x] 在 k6 module 中运行 `go test -race ./internal/resources ./cmd/resources`。
- [x] 删除根目录对应测试辅助包。

### Task 2: 收敛 runner 和文档

**Files:**
- Modify: `ghttp/testdata/k6/run.ps1`
- Modify: `ghttp/testdata/k6/run.sh`
- Verify: `ghttp/testdata/k6/README.md`
- Verify: `ghttp/testdata/k6/README.en.md`

- [x] 两个平台都从 k6 module 构建 `./cmd/resources`。
- [x] 删除 runner 对仓库根目录资源工具的依赖。
- [x] 运行 PowerShell/Bash 语法检查。
- [x] 运行完整 smoke，确认 resources、k6、rawprobe 和清理均通过。

### Task 3: 污染检查

- [x] 确认根目录不再存在本次新增的 `cmd/resources` 和 `internal/resources`。
- [x] 确认 `git status` 中所有测试正式文件均位于 `ghttp/testdata/k6`，根 README 徽章除外。
- [x] 运行 `go test -race ./ghttp/...` 和 k6 module `go test -race ./...`。

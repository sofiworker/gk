# 发布流程

> 仓库处于开发中（pre-v1.0.0），所有发布均为开发版，**禁止直接用于生产开发**（见 `DEVELOPMENT.md`）。

## 版本策略

- 遵循 SemVer；v0.x 阶段：
  - 破坏性变更 bump **minor**（v0.1 → v0.2）；
  - 新增/修复 bump **patch**（v0.1.0 → v0.1.1）；
- v1.0.0 之前不承诺 API 稳定，但每个破坏性变更必须记录在 `CHANGELOG.md` 并走 ghttp API 冻结 spec 的变更流程。

## 发布前检查

```bash
# 1) 全仓测试（最低支持工具链）
go test ./...

# 2) Go 1.27 预览工具链（泛型方法文件）
GOTOOLCHAIN=go1.27rc2 go test ./...

# 3) lint 与格式（go127 文件需 1.27 gofmt）
golangci-lint run ./...
test -z "$(GOTOOLCHAIN=go1.27rc2 gofmt -l .)"

# 4) race
go test -race ./...
```

CI（GitHub Actions）会执行等价门禁：legacy 矩阵、1.27 预览、gofmt、lint、race、coverage、webbench sanity。

## 发布步骤

1. 将 `CHANGELOG.md` 的 `[Unreleased]` 收敛为版本节（`[0.x.y] - 日期`），补充摘要；
2. 确认 `docs/superpowers/specs/2026-08-07-ghttp-api-freeze-v0.1.md` 的快照/弃用表与本次变更一致；
3. 提交并通过上述全部检查；
4. 打 tag：`git tag v0.x.y`，推送到远程（**需维护者明确授权**）；
5. 创建 GitHub Release，说明文字直接引用 CHANGELOG 对应节，并显著标注“开发中，禁止生产使用”；
6. 更新 README 中的版本/Go 版本说明（如适用）。

## 授权边界

- 未获维护者明确授权，禁止：推送 tag、发布 Release、部署生产、将本仓库依赖引入生产业务。

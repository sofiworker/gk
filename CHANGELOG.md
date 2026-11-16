# Changelog

本仓库遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.x 阶段 API 允许破坏性变更，但每个破坏性变更必须记录在对应版本节。

## [Unreleased]

### Changed

- **破坏性变更：** 项目更名为 Keel，根 module 从 `github.com/sofiworker/gk` 迁移到 `github.com/sofiworker/keel`，4 个独立的 `gotel/adapters/*` module 同步迁移。使用者需更新 Go import 和 `go.mod` 中的旧路径，然后运行 `go mod tidy` 与项目测试；子包名保持不变。迁移说明见中英文 README。
- OpenTelemetry 适配器默认 instrumentation scope 名称改为 `github.com/sofiworker/keel/gotel`；依赖旧名称的查询需更新，或通过 `WithInstrumentationName` 显式保留原名称。

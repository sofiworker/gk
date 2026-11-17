# 发布流程

[English](../release.md) | **简体中文** | [文档目录](README.md)

仓库仍处于 pre-v1.0.0 开发阶段，禁止直接用于生产开发，见[开发状态](development.md#开发状态)。

## 版本策略

遵循 SemVer。v0.x 阶段，破坏性变更提升 minor，兼容新增与修复提升 patch。
每个破坏性变更都记录在 [CHANGELOG.md](../../CHANGELOG.md)，同步更新相关指南与[迁移说明](migration.md)。

## 发布前检查

验证各模块最低支持的 Go 版本与较新受支持工具链：

```sh
make check
go test ./...
golangci-lint run ./...
go test -race ./...
```

使用 Go 1.27.1 检查和格式化带 build tag 的泛型方法文件。
4 个后端模块分别执行 tidy、vet、test 和 lint；目录及汇总目标见[开发检查](development.md#检查)。
对照当前源码确认 API 示例、默认值与 Go 版本要求。

## 多模块发布顺序

后端的 v0.0.0 依赖与本地 replace 只是开发占位，按下列顺序发布并固定真实版本：

| 顺序 | 模块 | Tag |
| --- | --- | --- |
| 1 | 根模块 | vX.Y.Z |
| 2 | 基础 OTel 适配器 | otel/adapters/otel/vX.Y.Z |
| 2 | 原生 Prometheus 适配器 | otel/adapters/prometheus/vX.Y.Z |
| 3 | OTLP 适配器 | otel/adapters/otlp/vX.Y.Z |
| 3 | OTel Prometheus 适配器 | otel/adapters/otelprometheus/vX.Y.Z |

基础 OTel 与原生 Prometheus 依赖根模块；OTLP、OTel Prometheus 还依赖基础 OTel。
下游不会继承本地 replace，应在独立消费方模块验证真实发布版本的安装。

## 发布步骤

1. 将 Unreleased 变更记录整理为带日期的版本节，列明破坏性变更。
2. 更新相关指南与迁移说明，检查文档链接。
3. 发布提交通过本地检查与远程 CI。
4. 获维护者授权后，创建并推送各模块所需 tag。
5. 创建 GitHub Release，描述变化并标明开发中、禁止直接生产使用。
6. 版本或 Go 要求变化时更新 README。

## 授权边界

打 tag、发布 Release 与部署均为需要授权的独立操作。本地修改、测试或功能文档本身
不构成发布授权；未获维护者明确授权，不得推送 tag、发布 Release、部署生产或将本仓库引入生产业务。

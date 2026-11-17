# 开发说明

[English](../development.md) | **简体中文** | [文档目录](README.md)

> 本文件是仓库级状态标记，优先级高于各主题文档中的功能描述。

## 开发状态

**整个仓库处于开发中（pre-v1.0.0），禁止直接用于生产开发。**

## 为什么

- API 尚未冻结，可能随时发生破坏性变更（包括删除、重命名、语义翻转）；
- 行为、默认值与文档仍在迭代，未形成稳定契约；
- 未经过生产环境规模、安全与兼容性验证；
- 部分模块依赖实验性工具链特性（例如 HTTP 的 Go 1.27 泛型方法与兼容实现）。

## 使用前提

- 仅允许用于内部实验、原型、学习与开发联调；
- 禁止直接引入生产业务或对外服务；
- 如确需使用，必须先与维护者确认版本、稳定性承诺与迁移路径，或等待 v1.0.0 正式发布；
- 任何以本仓库为基础对外发布、部署的行为，均视为未获授权。

## 仓库约定（对所有开发与 AI 代理生效）

- 所有改动默认不承诺向后兼容；破坏性变更必须在提交说明与 README 中显式标注；
- 未经维护者明确授权，不得向远程推送、打 tag、发布或部署；
- 文档中的“默认行为”仅代表当前实现，不代表最终契约。

## 仓库结构

```text
agent/                     模型/工具执行、会话、上下文与 Sandbox
cache/, config/, log/       应用工具
compress/, crypt/, gerr/     数据与错误工具
http/                       服务端、客户端、wire 与 HTTP 遥测
net/, poller/, resolver/     网络能力与运行时原语
otel/                       厂商无关的遥测契约
otel/adapters/*/            4 个独立后端 module
retry/, rx/, sd/, sql/       重试、反射、服务发现与 SQL
docs/                       英文主题文档
docs/zh-CN/                 同名中文文档
.github/workflows/go.yml    当前 CI
```

职责与依赖边界见[架构](architecture.md)，编码约定见 [AGENTS.md](../../AGENTS.md)。

## 检查

根模块和 4 个后端模块分别验证：

```sh
make check
go test ./...
```

在仓库根目录使用 POSIX shell 检查后端：

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

PowerShell 用户分别进入 4 个对应目录，执行同样的 3 条 Go 命令。
根模块测试不会进入嵌套 module；CI 还检查 lint、Go 版本、格式、race 与覆盖率。
带泛型方法的文件需要使用 Go 1.27+ 的 gofmt。

```sh
golangci-lint run ./...
go test -race ./...
```

各后端也分别执行 lint。根模块与各后端均通过 `go mod tidy -diff` 验证依赖，
集成测试条件见[平台支持](platform-support.md)。

`make check-adapters` 会在全部 4 个后端模块运行格式化、vet 和测试。
CI 同样使用 `otel/adapters/*` 源码目录。

## 文档约定

- 根 README.md 为英文项目入口，README.zh-CN.md 为中文入口。
- 详细英文指南放在 docs/，中文使用 docs/zh-CN/ 下的同名文件。
- 每篇主题页链接到另一语言版本和文档目录。
- 源码留在原目录，详细指南不在源码目录中重复维护。
- 移动文档时同步更新源码、许可证的相对链接及标题锚点。
- 按当前实现描述行为，区分 import 路径与 package 标识符。
- 新主题同时加入两份目录，示例及版本要求保持一致。

## 变更与发布

使用 Conventional Commits 并控制改动范围，破坏性变更写入[变更记录](../../CHANGELOG.md)
与[迁移指南](migration.md)。发布遵循[发布流程](release.md)，通过本地检查不代表获准发布。

# Changelog

本仓库遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.x 阶段 API 允许破坏性变更，但每个破坏性变更必须记录在对应版本节。

## [Unreleased]

### Added

- ghttp：Go 1.27 泛型方法 API（Server/Group 根组动词链、`ToNoInput`/`ToNoOutput`、builder 级 `Group`、`Client.Get/Post/Put/Delete` 类型化方法）。
- ghttp：WebSocket 生产化——Timeout 中间件支持 `Hijack`/`Flush`、raw 消息、context 感知读写、子协议协商、keepalive、路由级 Origin 覆盖、升级/处理错误日志。
- ghttp：SSE `WriteJSONWithID`（Last-Event-ID 续传）。
- ghttp：client 正式实现——重试、before/after 钩子、错误模型绑定、输出文件、BasicAuth/认证 scheme、查询参数/字符串、按请求超时、client cookies、Response 访问器、调试日志。
- 仓库：golangci-lint 配置与全仓 lint 清零；CI 增加 Go 1.27 预览、race、coverage、gofmt、lint 门禁。

### Changed

- ghttp：已配置 `Consumes` 时缺失 Content-Type 返回 415（原为按 JSON 解析）。
- ghttp：CORS 只对真正的预检请求（OPTIONS + Origin + Access-Control-Request-Method）短路。
- ghttp：Group 中间件在创建子组时快照（gin 语义），与 produces/consumes 快照一致。
- ghttp：RequestID 默认上限 128，超长客户端值替换为新 ID。
- ghttp：Timeout 超时取消 context 并丢弃迟到写入；writer 支持 Hijack/Flush。
- ghttp：路径参数提取单次化，提取错误走路由级错误管线；SSE handler 错误落日志；`Server.Use` 链式化。
- ghttp：client 钩子顺序固定为 client-before → request-before → 发送 → client-after → request-after。

### Deprecated

- ghttp（Go 1.27 构建）：`Route[Req,Resp](target)` 兼容 shim，新代码使用动词直挂。
- ghttp：`WithStrictContentNegotiation()` / `WithStrictContentType()` no-op 别名，计划 v0.2 移除。

### Removed

- ghttp：旧 `openAPIBuilder` 死代码、未用的 util/form 辅助函数、client 未用字段。
- gnet/rawcap：库内 demo `main` 文件（不做 demo，方向改为真实库）。

## [0.1.0] - 待发布

首版公开 API 快照（见 `docs/superpowers/specs/2026-08-07-ghttp-api-freeze-v0.1.md`）。开发中，禁止生产使用。

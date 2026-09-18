# Agent、模型与 HTTP 分层修正

状态修订：以下勾选项表示原型实现进度，不代表可交付 SDK。公开 API、默认组装及验证/授权位置由 [SDK 生命周期重设计](../specs/2026-09-18-gai-sdk-api-lifecycle.md) 重新定义；当前代码尚未实现其 Hook 管线。

- [x] 公共模型信息、参数、消息、结束原因与 Client/Catalog 小接口。
- [x] 厂商配置由独立 Protocol 持有，公共请求不携带厂商字段。
- [x] HTTP 收发与协议编解码分离，删除默认 JSONCodec。
- [x] 支持注入 ghttp 的标准 RoundTripper，不引入能力层依赖。
- [x] 基础 Runner 接入通用参数与结束原因。
- [x] Session/Turn 原子提交契约、内存 CAS、操作去重和提交日志。
- [x] Runtime 串联历史、默认独立 Sandbox、调用事实和取消。
- [x] HTTP 模型 → Sandbox 工具 → 结果回传 → 下一轮历史的端到端测试。
- [ ] 磁盘存储、审批恢复与进程重启恢复。
- [ ] 流式调用契约与实现。
- [ ] 独立厂商协议实现及兼容性验证。

Runner 提供执行循环，Runtime 负责会话级生命周期和原子提交；当前为同步非流式实现，内存状态不承诺崩溃持久性。厂商专属选项、部署地址与认证留在协议实现；逻辑模型身份不等于厂商模型名。破坏性变更：移除旧 model/httpclient Codec 入口，改为 modelhttp.Protocol 与独立 HTTP Transport 组合。

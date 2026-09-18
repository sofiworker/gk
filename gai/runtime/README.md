# Agent 运行时

[English](README.en.md)

开发中，禁止生产使用。Runtime 将 Agent 行为定义、厂商无关 model.Client、Session Store 与 Sandbox 接成完整的非流式执行链路。

`New(definition, client, store, options...)` 固定一个有版本的 Agent。`CreateSession(ctx, metadata)` 指定逻辑模型并默认分配独立内存 Sandbox。`Run(ctx, sessionID, Input{Content: ..., Parameters: ...})` 自动加载历史、创建 Turn、执行模型与工具循环、保存执行事实并返回 Turn；应用可从 HTTP handler 或后台任务调用。接入示例见 [example_test.go](example_test.go)，HTTP 与 Sandbox 全链路见 [runtime_test.go](runtime_test.go)。

Agent 不持有模型客户端或环境实例。Runtime 持有执行器；Session 关联环境；Turn 固定本轮实际绑定。WithAgentOptions 配置预算及工具授权，例如嵌套 agent.WithToolOptions(tool.WithAuthorizer(...))。注册工具不授予权限，默认拒绝工具执行。

默认环境仅存在本 Runtime 内存中。Sandbox(environmentID) 供可信控制层挂载资源、配置同步策略、快照与检查审计；每次工具调用只获得绑定 SessionID、TurnID、ToolCallID 的能力 View。路径不硬编码。WithEnvironment 注入外部环境解析器，返回实际环境版本和按调用构造的能力；解析器负责身份校验和授权，ID 本身不授予权限。

执行语义：

- 每次模型、工具操作前先提交运行中记录；提交失败不执行该操作。执行结果、关联消息和调用状态一起原子保存。失败调用也保留记录。
- 默认同一 Session 只有一个活动 Turn。进程内并发返回 ErrBusy；多个 Runtime 通过 Store 的版本检查和活动轮次约束竞争，冲突不会被覆盖。
- Run 使用调用者 context；Cancel(sessionID, turnID) 取消本实例的活动执行。取消后最多给予 5 秒记录事实，每次存储调用必须遵守 context。
- 工具发生错误后不自动重放。进入工具回调后返回错误，ToolExecution 可以为 unknown；这不表示没有副作用。批量工具请求尚有未配对结果时，后续 Run 返回 ErrHistory，不能直接把不完整对话发送给模型。
- 存储失败返回 agent.ErrCheckpoint。Store 中的最后状态可能仍为 running，返回的 Turn 可能包含尚未保存的事实；必须先核对实际副作用和存储结果。不会自动解锁或重新执行。
- 错误对象返回给调用者；持久化 Failure 使用固定英文说明，避免把上游错误中的凭据写入历史。

当前范围：非流式、同步 Run、默认内存环境和存储。没有进程重启恢复、审批等待/恢复、自动模型回退、分布式取消或工作流。等待审批的状态契约保留，但目前不自动进入该状态。默认内存存储不具备磁盘持久性；自定义 Store 必须实现原子提交、去重和并发约束。会话提交与 Sandbox/宿主机同步不是同一事务，回滚或同步由可信控制层显式调用。

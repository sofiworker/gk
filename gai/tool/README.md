# Tool

[English](README.en.md)

开发中，禁止直接用于生产开发。`core.Agent.Tools` 直接持有 `[]Tool`；本包提供工具集构建、模型可见描述和执行分发，不包含模型循环。

## 分工

| 对象 | 职责 |
| --- | --- |
| `core.ToolDefinition` | 模型可见 Name、Description、InputSchema；Version 用于固定实现 |
| `core.Tool` | 提供描述、无副作用 Validate、实际 Execute |
| `tool.Set` | 固定本次执行可用的工具集合，按模型返回的 Name 分发 |
| `core.ToolContext` | 当前调用归属与受限文件、网络、资源申请能力 |

工具 Name 使用 1–64 位字母、数字、下划线或连字符，同一个 Set 中必须唯一。New 复制并固定描述与版本；修改 Agent.Tools 切片不会改变已有 Set。执行记录使用 core.Binding，ID 为工具 Name，Version 为构建时固定的版本，仅用于追溯，不用于查找实现。

## 构建与执行

```go
agent := core.Agent{ID: "reader", Tools: []core.Tool{readTool}}
set, err := tool.New(agent.Tools, tool.WithAuthorizer(authorize))
if err != nil {
    return err
}
// set.Definitions() 由模型适配器转换为模型的工具声明。
call := core.ToolCall{
    ID: "model-call-1", Name: "read_file",
    Arguments: json.RawMessage(`{"path":"/project/file.txt"}`),
}
attribution := core.Call{
    ID: "execution-1", SessionID: "session-1", TurnID: "turn-1", ToolCallID: call.ID,
}
view := box.View(attribution)
execution, err := set.Execute(ctx, core.ToolContext{Call: attribution, Files: view}, call)
```

`readTool` 实现 core.Tool；`authorize` 是可信应用的 `func(context.Context, tool.Authorization) error`。返回 nil 表示允许，返回错误表示拒绝，可按业务策略自动执行，不要求人工审批。未配置 Authorizer 时默认拒绝。传入哪些能力由可信应用决定，在 Agent 中声明工具不自动授予宿主或网络访问。

执行顺序：工具集查找 → JSON 对象与大小检查 → Tool.Validate → Authorizer → Tool.Execute。参数与描述复制，校验器或授权器修改副本不会篡改真正执行的参数。重复 JSON 键、多个 JSON 值和过深嵌套会被拒绝。授权器接收工具版本、参数和执行归属，不接收环境能力接口。

`Execution` 返回实际工具名称与版本、CallID、是否进入回调、状态、时间及 ToolOutput。执行回调开始后的 Go 错误、panic 或取消标记为 unknown；不能据此断言没有外部副作用。业务 ToolOutput.Error 标记 failed。不会自动重试，也不会自动写入 Session。Runtime 负责生成独立执行 ID、记录审批和历史，并将结果与 ToolExecution/ToolResult 关联。

同一 ToolCall 再次调用 Execute 是新的执行尝试；本包不提供 exactly-once。文件幂等由 Sandbox 操作 ID 负责，多次文件操作必须使用各自的稳定操作 ID，不能将一个 ID 用于不同写入。

## 函数适配

`NewFunction[T](definition, validate, run)` 将类型化 Go 回调适配为 core.Tool。validate 与 run 均必须提供；validate 负责 required、枚举、取值范围等业务规则，不得产生副作用。run 接收 context、ToolContext 和解码后的 T，返回原生多模态 ToolOutput。

Schema 必须显式提供为 type=object 的有效 JSON。本包不推导 Schema，也不提供完整 JSON Schema 校验。函数适配器拒绝结构体未知字段，使用 json.Number 保留动态数字；缺失字段、null、约束关键词是否允许由 validate 明确检查。Schema 与 Go 类型的一致性由工具作者保证。

## 生命周期与边界

Set 支持并发执行；返回的描述是副本。Agent 直接持有实现对象，必须保持行为版本稳定并支持并发复用，不把某个 Session 的环境保存在共享对象中。需要不同服务配置时创建不同实例。

默认参数上限 1 MiB、调用期限 30 秒，使用 WithLimits 修改。期限覆盖验证、授权和执行，但属于协作取消，不能强杀同一进程里不遵守 context 的 Go 回调。不会启动后台 goroutine 然后提前报告工具已停止。任意自定义 Go 工具仍是可信宿主代码。

首版不实现 MCP、动态 Toolset、流式工具、自动工具发现、输出 Schema 验证或模型 Runtime。这些可以在当前描述/执行分离与显式上下文基础上扩展。

## 内置文件工具

[builtin](builtin/README.md) 提供六个基础文件工具构造函数，可按需放入 Agent.Tools。工具通过受控文件接口执行；搜索与命令兼容性仍是基础版本。

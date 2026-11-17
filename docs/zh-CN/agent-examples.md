# Agent 示例

[English](../agent-examples.md) | **简体中文** | [文档目录](README.md)

可运行示例以实现旁的 Go Example 测试维护，使用本地 fake 或内存组件，不需要模型 API 凭据。

| 示例 | 源码 | 内容 |
| --- | --- | --- |
| 会话执行 | [Runtime 示例](../../agent/runtime/example_test.go) | Agent 定义、fake 模型、内存 Store、创建 Session 与 Run |
| 工具 | [工具示例](../../agent/tool/example_test.go) | 构建工具与显式执行 |
| 会话行为 | [Runtime 测试](../../agent/runtime/runtime_test.go) | 记录、并发、取消与失败边界 |

在仓库根目录执行：

```sh
go test ./agent/runtime ./agent/tool -run Example -v
```

更完整的 SDK 测试命令为 `go test ./agent/...`。
当前支持范围见[运行时](agent-runtime.md)、[工具](agent-tools.md)、
[上下文](agent-context.md)与 [Sandbox](agent-sandbox.md)；
流式、进程重启恢复及审批挂起/恢复尚未实现。

## 拟议 API 设计示例

原始设计示例保留在 `agent/examples/`，使用 `//go:build ignore` 排除构建，当前不能运行。
其中的 `model.Model`、选项式 `agent.New`、`hook` 和类型化输出 API 尚未实现。
示例通过参数接收应用配置的模型；Sandbox 示例借用调用方提供的环境，生命周期由调用方管理。

| 源码 | 拟议行为 |
| --- | --- |
| [agent.go](../../agent/examples/agent.go) | 声明 Agent 并执行单次请求 |
| [session.go](../../agent/examples/session.go) | 连续会话，自动使用历史 |
| [sandbox.go](../../agent/examples/sandbox.go) | 绑定 Sandbox、文件工具与工具授权 |
| [hooks.go](../../agent/examples/hooks.go) | 请求前配置参数，响应后转换与业务校验 |
| [structured_output.go](../../agent/examples/structured_output.go) | 基于反射处理结构体、指针、切片、数组、Map 和空对象的输出 |

类型化输出采用非泛型、反射式写法，拟议签名为 `JSONType(sample any) Spec` 和
`Bind(target any) Spec`。JSONType 只生成 JSON Schema，不绑定响应；Bind 还会把验证后的
最终响应回填到非空、可写指针。两者均只读取运行时类型，不发送 sample 的字段值。
支持 string-keyed map 与 typed nil；空接口 `nil` 没有运行时类型，在模型请求前报错。
验证或解码失败时保持回填目标不变。

这些是待实现 API 的用法契约。接口落地后应移除构建排除，并加入编译与执行测试。

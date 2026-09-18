# Go 示例

类型化输出的非泛型、反射式写法见 [structured_output.go](structured_output.go)：

| 目的 | API |
|---|---|
| 只生成 JSON Schema | `output.JSONType(Person{})` |
| Schema + 响应回填 | `output.Bind(&person)` |

拟议签名为 `JSONType(sample any) Spec` 和 `Bind(target any) Spec`。JSONType 只读取 `reflect.Type`，不发送 sample 的字段值，也不绑定响应；Bind 要求非空、可写指针，验证和解码成功后才回填，失败时保持目标不变。支持结构体、指针、切片、数组、string-keyed map 以及 typed nil；空接口 `nil` 没有运行时类型，会在模型请求前报错。当前仍是设计接口，尚不可运行。

[English](README.en.md)

新 API 的 Go 源码示例：

- [agent.go](agent.go)：声明 Agent 并执行单次请求。
- [session.go](session.go)：连续会话，自动使用历史。
- [sandbox.go](sandbox.go)：绑定 Sandbox、声明文件工具和工具授权。
- [hooks.go](hooks.go)：请求前配置参数，响应后转换与业务校验。
- [structured_output.go](structured_output.go)：结构体、指针、切片、数组、Map、空对象和失败回填示例。

这些文件使用新设计的接口。`model.Model`、选项式 `agent.New` 和 `hook` 等尚未实现，因此标有 `//go:build ignore`，当前不能运行。代码通过函数参数接收应用配置的模型，不虚构厂商客户端。Sandbox 示例借用调用方提供的环境，生命周期仍由调用方管理。

这些是待实现 API 的用法契约，不是已完成 SDK 的演示。接口落地后应移除构建排除并加入编译、执行测试。

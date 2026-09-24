# v2 review 导航

v2 按普通 HTTP 主线开发，不增加 v1 API 兼容层，不改写 gmemos。代码生成和 Go unsafe 禁止。来源 query 访问复用底层请求能力。

建议按顺序 review：

1. route.go：包级泛型门面、Option、Route.Err/Serve、WithMiddleware、WithBodyLimit。所有 HTTP 方法共用执行器，值/指针类型由函数推断。
2. binding.go：默认输入与来源标签，注册期解析计划；显式 WithInput 替换默认计划。
3. contracts.go：Input/Output、自定义 codec 与内置 codec，默认 JSON，文本/HTML显式选择。
4. multipart.go：普通字段、单/多文件、总大小与驻留内存限制；route.go 管理临时文件生命周期。
5. bridge.go：无输入/仅 error 适配器、Reply 元数据、Mount 到现有路由器。
6. *_test.go：端点执行、真实 HTTP 挂载、指针组合、输入错误、multipart 清理和编码输出。

新增 server.go 独立 HTTP 服务门面，validation.go 输入校验，negotiation.go 显式协商，form_plan.go 和 multipart.go 注册期计划，multipart_stream.go 流式上传，http_reply.go 下载/流/重定向，openapi.go 注册元数据导出。批量安装不承诺底层失败回滚。

性能事实：无代码生成，无 reflect.Call。绑定计划并不等于零反射，字段访问、指针分配与标准 JSON/XML 编解码仍有请求期成本；表单和 multipart 标签读取在注册期完成。bench_test.go 分离注册与执行，执行基准包含 httptest.NewRecorder 的分配，不能作为与其他框架的性能比较。

需要 review 决策的语义：

- 无标签请求默认为整体 JSON；标签输入最多一个 body 字段。
- 显式 codec 类型与 handler 不匹配为注册错误，而不是编译错误。
- 缺省 Content-Type 当前允许；显式不匹配拒绝为 415。
- 默认请求体上限为 32 MiB；multipart 另有独立解析限制。
- 输入根支持 T/*T，不支持多级指针；文件句柄生命周期止于端点结束。
- Action 默认 204，普通 nil 输出为 JSON null；已有 200+{} 协议需显式配置。

Group review: `group.go` adds API/Group, nested prefix, typed input/output defaults, body-limit default and middleware inheritance. `GetIn`/`PostIn`/other `*In` helpers construct and mount one route; `Routes` prefixes prebuilt routes. `Group.Mount` decorates one prebuilt route with prefix and middleware. Batch installation is not transactional. Generic methods are avoided for Go 1.25 compatibility.

新增推荐入口：New(server) → Group(...).With(...).Use(...) → Register(routes...)。Register 与 Routes/Group.Mount 共用最终编译流程，合并所有组默认值；批内配置错误预检，不承诺底层安装事务。旧性能报告早于此次注册流程修改，注册数据不可作为当前结果。

# v2 普通 HTTP 主线验收清单

v2 作为后续主线设计，不提供 v1 API 兼容层。复用底层路由实现不等于承诺 v1 API 兼容。禁止代码生成和 Go unsafe；极限性能选项仅用于显式转移验证责任。排除 SSE、WebSocket，不改 gmemos。

## 当前执行计划

- [完成] 生命周期：所有输入路径的 Body 限制、multipart 清理、失败及 panic、HEAD 与响应提交语义。
- [完成] 来源绑定：嵌套结构体/指针、重复值、标量与 slice、自定义文本转换、注册错误与请求错误。
- [完成] 输出：JSON/XML/text/HTML、Reply 元数据、重定向、流式响应、下载/Range/条件请求。
- [完成] 显式输入校验 option：类型检查、组继承、解码后调用、失败归类。
- [完成] 表单与 multipart 注册期字段计划；presence 使用指针，默认值与必填规则交给 WithValidator，不额外引入隐式标签 DSL。
- [完成] 内容协商：Accept 权重与通配符、406/415、Vary、可选严格解码。
- [完成] 普通 multipart 流式上传及资源生命周期。
- [完成] 独立 v2 Server 门面：启动/关闭、超时、路由/Group、405/OPTIONS/HEAD、middleware及配置。
- [完成] 注册元数据与 OpenAPI、批次注册原子性边界。
- [完成] 全矩阵回归、真实网络测试、race、性能与使用文档更新。

## 明确边界

- 这是普通 HTTP 服务端主链路的可 review 实现，不表示所有 HTTP 扩展或所有 OpenAPI 特性已实现。
- 批次预检失败不安装；底层安装失败可能保留已安装路由，不提供事务回滚。
- OpenAPI 显式生成，记录成功注册路由；无法准确推断的自定义 decoder/响应、XML/multipart schema 与协商响应标记未知，不生成虚假结构。
- 静态 HTML 输入输出是文本；模板引擎、业务认证策略由应用提供。文件目录服务可由应用组合 FileReply，不默认暴露文件系统。
- 源码内核仍复用根包路由/请求/错误处理；没有新增 v1 API 兼容层，也未删除旧包。
- 未引入 SSE、WebSocket、代码生成、Go unsafe 或外部依赖。

## 验证

v2 单元测试、race、make check 与 HTTP 矩阵内容契约通过。全仓测试在允许本地监听的环境执行。矩阵原始结果见 benchmarks/results/facade-mainline-2026-09-24.txt；40 MiB 短采样次数过少，不据此做性能排名。


静态检查边界：make check 内的 go vet 通过。独立 golangci-lint 未运行成功：本机安装 v1，而仓库配置要求 v2；没有更换工具或修改仓库 lint 配置绕过检查。

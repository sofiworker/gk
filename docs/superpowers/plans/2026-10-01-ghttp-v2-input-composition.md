# v2 输入组合实施计划

目标：保持 `func(context.Context, I) (O, error)`，让类型化 body 与按需来源读取自然组合，不通过反射调用任意 handler。

统一请求包装调整：BodyInput[T]/Body/WithBodyValidator 改为 RequestOf[T]/DecodeRequest/WithDataValidator，字段 Body 改为 Data。JSON、form、multipart、XML 和流式输入共享包装，格式由 codec 选择。中英文 README 明示破坏性变更。

- [x] 统一公开命名、注册工厂和测试。
- [x] 更新中英文迁移说明及表单示例。
- [x] 复验统一包装的测试、race 和 make check。

- [x] 新增 `RequestOf[T]`，默认 JSON；显式 `DecodeRequest(codec)` 复用现有 codec。包装在注册期选择，请求期直接构造。
- [x] 新增 `ReadBody` 供 RequestInput 显式按需解码、`RequireBody` 表达必需 body、`WithDataValidator` 复用业务 body 校验。
- [x] 保留 body schema，新增不触发绑定的参数文档声明。
- [x] 增加组合、错误、校验顺序、资源清理、schema、独立请求与基准测试。
- [x] 更新中英文 README，验证相关包及仓库规定的检查，记录限制。

边界：无输入继续使用 FromFunc/FromProcedure；自动 DTO 绑定继续可选使用。JSON null 使用 codec 原有语义；字段缺失与 null 的区分由业务字段类型负责。不自动缓存或重放 body；流式 reader 和请求视图仅在当前请求有效。不改路由器，不增加依赖，不承诺性能追平其它框架。

验证环境：Windows/amd64，Go 1.26.4。全仓 `go test ./...` 在既有 `ghttp/graceful_test.go` 引用 Windows 不支持的 syscall.SIGUSR1 处编译失败。离线 `go mod tidy -diff` 缺少 otel/sdk/metric 和 gonum 的依赖测试缓存，未更改依赖文件。golangci-lint 未安装。为支持当前平台验证，既有 multipart 清理测试同时设置 TMPDIR/TMP/TEMP。

最终检查：`make check PKGS=./ghttp/v2`（fmt/vet/test/分层检查）通过；`go test -race ./ghttp/v2 ./ghttp/v2/examples/http` 通过；`git diff --check` 通过。

新增 BenchmarkRequestOfComposition 对照完整 DTO、RequestOf、ReadBody：均读取 id/page/name，响应预检一致，每轮复制请求、重建 body 和响应 header。当前短 JSON 样例为 DTO 1905 B/19 次，RequestOf 与 ReadBody 1873 B/19 次。最终性能采样与 race 校验同时运行，耗时不可用于严格排名；这里仅记录分配结果，不声称普遍提速或 Gin/Echo 性能持平。

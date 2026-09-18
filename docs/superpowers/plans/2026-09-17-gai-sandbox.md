# gai 内存 Sandbox 实施计划

- [x] 收敛 Agent 与环境执行事实，定义小接口和稳定错误。
- [x] 实现内存文件、访问规则、审计、幂等、配额和快照。
- [x] 实现资源提供、宿主导入、固定批次同步及冲突恢复。
- [x] 实现受控 HTTP、资源申请与工具能力视图。
- [x] 补充行为测试、中英文 README，运行相关 race 测试、lint、make check 与全库测试。

范围：实现 Sandbox 设计前三阶段中独立于模型 Runtime 的能力。当前仓库尚无模型执行 Runtime，阶段/会话结束同步由显式 Sync 入口供应用调用；本次不实现模型协议或完整 Agent 执行循环。全程不新增第三方依赖，不提交或推送。

## 完成记录

- Agent 删除环境相关字段；EnvironmentBinding 统一会话、轮次和审批中的环境执行事实。
- Tool.Execute 显式接收 ToolContext。共享环境契约置于 core，sandbox 用类型别名提供领域入口，遵守现有 core 不反向引用功能包的检查。
- 内存实现、Unix 宿主适配、受控 HTTP、资源申请、差异查询、固定快照同步与生命周期均已实现；中文/英文 README 标注破坏性变更及使用边界。

## 验证结果

- `make check PKGS=./gai/...`：通过。格式化、vet、相关测试、全库依赖分层检查均通过，避免格式化无关模块。
- `go test ./...`：通过。
- `go test -race -coverprofile=/tmp/gai-sandbox-coverage.out ./gai/...`：通过。httpgate 84.3%、local 81.7%、memory 82.7% 语句覆盖率。
- `GOOS=windows GOARCH=amd64 go test -exec /bin/true ./gai/...`：交叉编译通过；没有执行 Windows 二进制，不代表 Windows 运行验证。local 非 Unix 平台明确拒绝宿主适配。
- `git diff --check` 与相关 Go 文件格式检查：通过。
- `golangci-lint run ./gai/...`：环境阻塞，本机仅 v1.64.8，仓库使用 v2 配置。已有 standalone staticcheck 同样不能读取 Go 1.27 导出数据，未把 lint 记为通过。
- `GOPROXY=off GOSUMDB=off go mod tidy -diff`：离线环境缺少已有 gsd → etcd/grpc 测试依赖 go.opentelemetry.io/otel/sdk/metric v1.44.0；未联网下载。go.mod/go.sum 未改动，本次实现只增加标准库引用。

模型 Runtime、存储、通知及自动阶段/会话完成触发不在本次实现范围；通过明确的 Sync、资源申请管理与 ToolContext 接口接入。未提交或推送。

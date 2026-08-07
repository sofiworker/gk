# 注释/文档双语化进度

规范见根目录 `AGENTS.md`：注释与文档中英双语（中文在前、英文在后），冗余注释直接删除。

## 已完成

- 库文件：gresolver/parser.go、gsql（scan/migrate_source/dialect）、gcache（memory/timed/lru/lfu/valkey）。
- 测试：gretry、grx、gconfig、gresolver、gcrypt、gsd、glog、gsql（extra/builder）、gcache（全部）、gcompress（全部）。
- ghttp 小文件（每文件 1-4 条注释）：codec*/cookie/logger/params_test/render/schema/output/slog/upload/util/vfs/writer/feature_coverage/openapi_compiler 等。
- ghttp 中等文件：error/group/error_writer/gerr/input/authz/sse/validate/handler。

## 待办（按批次）

- [x] gcache 测试、gcompress 测试
- [ ] ghttp 大文件（codec_manager/doc/websocket/server/middleware/builder_core/builder_go127/builder_pre127/client/config/params/route_* 等）
- [ ] gnet（26 个文件）
- [ ] README 双语化（当前为中文为主）

处理原则：优先删除复述型/标签型注释；有信息量的注释转双语；不修改功能代码。

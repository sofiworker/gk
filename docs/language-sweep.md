# 注释/文档双语化进度

规范见根目录 `AGENTS.md`：注释与文档中英双语（中文在前、英文在后），冗余注释直接删除。

## 已完成

- 库文件：gresolver/parser.go、gsql（scan/migrate_source/dialect）、gcache（memory/timed/lru/lfu/valkey）。
- 测试：gretry、grx、gconfig、gresolver、gcrypt、gsd、glog、gsql（extra/builder）、gcache（cache/timed，标签已删，部分说明仍待补双语）。

## 待办（按批次）

- [ ] gcache/lru_test.go、gcache/lfu_test.go：说明性注释双语化
- [ ] gcompress/*_test.go：边界/说明注释双语化
- [ ] ghttp（61 个文件）
- [ ] gnet（26 个文件）
- [ ] README 双语化（当前为中文为主）

处理原则：优先删除复述型/标签型注释；有信息量的注释转双语；不修改功能代码。

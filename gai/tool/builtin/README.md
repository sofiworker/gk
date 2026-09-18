# 内置文件工具

[English](README.en.md)

开发中的基础实现，禁止直接用于生产开发。提供 NewListDir、NewReadFile、NewFindFiles、NewSearchText、NewWriteFile、NewEditFile，均返回 `(core.Tool, error)`。调用方选择工具放入 Agent.Tools，然后通过 tool.New 构建执行集合并显式配置授权。

所有访问经过 ToolContext.Files，不持有 Session、不启动宿主 rg/find/sed 或 shell。

| 工具 | 基础语义 |
| --- | --- |
| list_dir | 列举直接子项，按路径排序 |
| read_file | UTF-8 文本按行读取，行号从 1 开始 |
| find_files | 递归查找普通文件；Go path.Match，不支持 **；无斜杠模式匹配文件名，有斜杠模式匹配完整虚拟路径 |
| search_text | 字面量/Go 正则搜索，支持忽略大小写、路径过滤、上下文行 |
| write_file | 创建或覆盖文本，父目录必须存在 |
| edit_file | 精确字符串替换，默认唯一匹配，replace_all 才允许多处替换 |

默认返回 200 条/行，上限 1000；遍历预算 10000 项，文件及搜索总文本预算 8 MiB，JSON 输出上限 256 KiB。条数截断携带 truncated；字节、扫描预算超限返回 ErrQuota。读取拒绝二进制，搜索跳过二进制；无权限或其他读取错误直接返回，不宣称完整 rg/sed 兼容。工具返回 JSON 文本；修改结果保留 LocalApplied/SyncState，即使宿主同步失败也可判断内存是否已经修改。

edit_file 改编自 Eino InMemoryBackend.Edit 的唯一匹配检查与字符串替换逻辑，新增本 SDK 接口适配和原子内容校验。memory.Sandbox 与 View 提供 CompareAndWrite；不支持该能力的文件后端返回 ErrUnsupported，不退化为不安全的读后直接写。expected_content_id 可选，未提供时仍按读取内容的 SHA-256 校验写入。

当前实现用于后续迭代，未包含 glob 双星、流式读取、分页游标、完整 POSIX 或命令参数兼容。

## 第三方来源

编辑替换部分来源：CloudWeGo Eino，`adk/filesystem/backend_inmemory.go` 的 Edit 方法，本地版本 `9d983b36`，Copyright 2024 CloudWeGo Authors，Apache-2.0。改编说明及版权头保留于 edit.go；许可证全文见 [LICENSE-APACHE](LICENSE-APACHE)。未引入 Eino 或其他第三方运行依赖，其余工具为本地接口适配实现。

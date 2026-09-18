# 会话存储

[English](README.en.md)

Reader、Creator、Writer 是可独立实现的小接口，Store 将其组合。Create 接受无 Turn、Version 为零的新会话，存储初始化版本。Commit 以 SessionID、ExpectedVersion、OperationID 提交一个 Turn 的完整当前状态；消息正文只存在 Session.Turns.Messages 中，调用记录用 ID 关联。

[memory](memory/store.go) 是默认内存实现：原子检查版本与活动 Turn，同一操作 ID 和同一内容重发返回原版本，不同内容冲突；拒绝改写旧消息、终结调用或已结束 Turn，保留提交日志 History。读取、提交和日志均复制数据。它没有磁盘持久性、容量限制或日志压缩，仅适合开发联调；生产状态见根目录 DEVELOPMENT.md。

Store 是可信运行时的存储边界，不负责工具权限、协议内容校验或外部副作用的 exactly-once。CAS 不能让模型调用、工具操作和宿主机同步成为同一事务。

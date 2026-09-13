# gai/id

[English](README.en.md)

`id.NewV7()` 返回标准小写 UUID v7 字符串，供 Session、Turn、Message 和调用记录使用。模型返回的 RequestID、ToolCallID 保留原值。

实现改编自 google/uuid v1.6.0 的 version7.go，保留 [BSD 许可证](LICENSE)。裁剪为字符串生成接口，使用标准库 crypto/rand，不包含随机池和解析 API。兼容仓库最低 Go 1.25.12；Go 1.27 虽提供 uuid 包，此处不引入更高版本要求。

时间部分为 Unix 毫秒，使用子毫秒序列与进程内互斥锁处理重复时刻和时钟回退。顺序仅指临界区内生成顺序，不保证并发返回顺序、跨进程顺序或重启后的单调性；序列推进可能使时间部分略领先墙上时钟。消息排序及 CAS 仍使用序号和版本。

遵循 Go 1.25 crypto/rand.Read 契约，随机源不可用时由标准库终止进程，不回退到不安全随机数。

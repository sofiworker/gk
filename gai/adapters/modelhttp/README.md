# 模型 HTTP 组合

[English](README.en.md)

New(protocol, transport) 组合独立的模型协议与 HTTP 传输。Generate 先校验公共参数，再依次编码、发送和解码。协议实现负责逻辑模型到厂商模型的映射、独立厂商配置、认证、消息转换、状态码错误、用量和结束原因归一化。

没有默认 JSON 网关协议，也没有内置厂商实现。HTTP 传输不自动重试，协议解码失败不会自动重发请求。非 HTTP 模型实现可以直接实现 model.Client。

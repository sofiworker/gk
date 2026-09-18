# HTTP 传输

[English](README.en.md)

仅处理 HTTP 方法、URL、请求头、正文、状态码与响应头，不认识模型或厂商。响应状态码原样交给协议实现处理。默认超时 60 秒、请求与响应各限制 8 MiB、不自动重试、不跟随重定向、不读取环境代理。

通过 WithTransport 注入标准 http.RoundTripper。例如应用组合层可传入现有 ghttp/client 的 Client.RoundTripper()，复用传输配置；这不会执行 ghttp 中间件、状态码判定或请求重试编排，gai 本身不依赖 ghttp。注入传输属于可信实现，可以自行改变底层代理等行为。

当前缓冲完整响应，不支持流式或 SSE。Close 关闭支持该能力的传输的空闲连接；共享传输的生命周期由调用方协调。

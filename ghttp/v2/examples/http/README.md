# v2 完整 HTTP 示例

[English](README.en.md)

从仓库根目录启动：

```sh
go run ./ghttp/v2/examples/http -addr 127.0.0.1:8080
```

按 Ctrl+C 触发优雅关闭，最多等待 5 秒。此示例用于开发验证：不连接数据库，不持久化用户修改；上传内容读取后丢弃，只返回字节数。固定 Bearer token 仅演示组中间件，不是实际认证方案。仓库仍处于实验开发阶段。

## 对照代码

- `newServer`：独立 Server、全局 Recovery、Group、组级 BodyLimit、路由注册、OpenAPI。
- `readUser` / `updateUser`：值/指针 DTO、path + JSON body、validator、JSON/XML 内容协商。
- `inspect`：直接接收 RequestInput，按需读取 query/header/cookie，不需要 DTO。
- `decodePath`：显式 DecodeWith；不关闭默认防护。
- `upload` / `streamUpload`：multipart DTO 与逐 part 上传。
- `download`：FileReply、附件文件名、Range/HEAD/Last-Modified。
- `/stream`：普通 HTTP 流式输出；不是 SSE。
- `/users` POST：StrictJSONInput、Reply 状态码及响应头。
- `/users/{id}` DELETE：Action 默认 204。
- `/`：RedirectReply。

## 请求演示

```sh
# 无需鉴权
curl -i http://127.0.0.1:8080/health
curl -i http://127.0.0.1:8080/

# 自动绑定 path，默认 JSON
curl -i -H 'Authorization: Bearer demo' http://127.0.0.1:8080/api/users/7

# 同一个 handler 输出 XML
curl -i -H 'Authorization: Bearer demo' -H 'Accept: application/xml' http://127.0.0.1:8080/api/users/7

# path + JSON body 自动绑定；name 为空返回 400
curl -i -X PATCH -H 'Authorization: Bearer demo' -H 'Content-Type: application/json' \
  -d '{"name":"alice"}' http://127.0.0.1:8080/api/users/7

# 严格 JSON 输入，返回 201；额外未知字段返回 400
curl -i -H 'Authorization: Bearer demo' -H 'Content-Type: application/json' \
  -d '{"id":8,"name":"bob"}' http://127.0.0.1:8080/api/users

curl -i -X DELETE -H 'Authorization: Bearer demo' http://127.0.0.1:8080/api/users/7

# 增强 Request 输入
curl -i -H 'Authorization: Bearer demo' -H 'X-Trace: trace-1' -b 'session=example' \
  'http://127.0.0.1:8080/api/inspect?q=hello'

# 自定义解码器
curl -i -H 'Authorization: Bearer demo' http://127.0.0.1:8080/api/decoded/9

# URL 编码表单和重复值
curl -i -H 'Authorization: Bearer demo' -d 'name=alice&tag=a&tag=b' http://127.0.0.1:8080/api/form

# 在仓库根目录运行；普通上传和流式上传均限制整个请求为 2 MiB
curl -i -H 'Authorization: Bearer demo' -F 'title=example' -F 'file=@go.mod' http://127.0.0.1:8080/api/upload
curl -i -H 'Authorization: Bearer demo' -F 'file=@go.mod' http://127.0.0.1:8080/api/upload-stream

curl -i -H 'Authorization: Bearer demo' http://127.0.0.1:8080/api/download
curl -i -H 'Authorization: Bearer demo' -H 'Range: bytes=0-4' http://127.0.0.1:8080/api/download
curl -I -H 'Authorization: Bearer demo' http://127.0.0.1:8080/api/download
curl -i -H 'Authorization: Bearer demo' http://127.0.0.1:8080/api/stream

curl http://127.0.0.1:8080/openapi.json
```

OpenAPI 在业务路由注册后生成一次，不包含随后安装的文档端点本身。动态输入、协商输出等无法完整推断之处明确标记，不替代业务 API 文档。

文件示例使用内存 reader，无需关闭。改成真实 os.File 时，FileReply 同时设置 Content 与 Closer；不要在 handler 中 defer Close，因为响应编码发生在 handler 返回之后。

验证：

```sh
go test ./ghttp/v2/examples/http
go test -race ./ghttp/v2/examples/http
```

直接响应 handler 示例：`POST /api/raw` 保留组鉴权和 BodyLimit，手动读取并原样写出文本，返回写入错误：

```sh
curl -i -H 'Authorization: Bearer demo' --data-binary 'hello' http://127.0.0.1:8080/api/raw
```

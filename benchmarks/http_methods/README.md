# ghttp、Gin、Echo 请求方法基准

这个目录比较三种框架在相同请求语义下的进程内处理开销。所有框架都通过常规路由注册、路径/查询/请求头读取、JSON 绑定和 JSON 输出完成处理，没有使用 `Raw`、底层 `ResponseWriter` 直写或框架内部上下文接口。

覆盖场景包括：

- GET 静态路由、路径参数 + query + header
- POST、PUT、PATCH JSON 请求体绑定和 JSON 响应
- DELETE 路径参数
- HEAD、OPTIONS
- 三层中间件和 query 组合
- 未匹配路由

运行 sanity 检查：

```powershell
go test ./http_methods -run TestMethodMatrix -v
```

运行基准：

```powershell
go test ./http_methods -run '^$' -bench BenchmarkHTTPMethods -benchmem -benchtime=2s -count=5 -cpu=8
```

请求和响应对象在循环外复用，响应 body 丢弃到固定 writer；因此结果关注框架处理链，避免 `httptest.ResponseRecorder` 和网络 I/O 混入测量。

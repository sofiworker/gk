# ghttp v3

[English](README.en.md)

实验性 pre-v1 API，禁止直接用于生产开发。v3 是独立包，暂时保留 v2；不兼容替换 v2，不承诺 API 已冻结。

## 统一请求模型

普通 HTTP 方法构造器的泛型参数是 Data 类型 T 和输出类型 O，handler 固定为 `func(context.Context, RequestOf[T]) (O, error)`。RequestOf[T] 提供按需请求视图和 Data，不按 JSON/form/multipart 增加包装类型。

```go
import httpv3 "github.com/sofiworker/gk/ghttp/v3"

type UpdateBody struct {
    Name string `json:"name"`
}

func updateUser(ctx context.Context, req httpv3.RequestOf[*UpdateBody]) (User, error) {
    id := req.Path("id")
    notify, _ := req.QueryFirst("notify")
    return service.Update(ctx, id, req.Data, notify == "true")
}

server := httpv3.NewServer()
api := server.Group("/api")
err := api.Register(httpv3.Patch("/users/{id}", updateUser))
```

T/O 通常从 handler 推断；也可以显式写 `Patch[*UpdateBody, User]`。Get/Head/Post/Put/Patch/Delete/Options/Method 共用这一签名；普通端点不接收任意 I 或指针请求包装。Data 可为值或指针。

## 输入格式与按需读取

默认只把 body 按 JSON 解码到 Data，不隐式绑定 Data 中的 path/query 标签。来源参数由 Path、QueryFirst/QueryValues、HeaderValue、CookieValue 按需读取。字符串访问不做业务类型转换；未读取的非法业务参数不会被自动拒绝，转换失败由 handler 或显式绑定处理。路由匹配和 path 参数捕获仍在 handler 前完成。

```go
func listUsers(ctx context.Context, req httpv3.RequestOf[httpv3.NoData]) ([]User, error) {
    page, present := req.QueryFirst("page")
    return service.List(ctx, page, present)
}
route := httpv3.Get("/users", listUsers)
```

NoData 明确跳过默认输入解码，客户端 body 不会被自动读取；不使用普通 struct{} 代替它。显式 WithInput 可覆盖 NoData 默认值。完全不需要请求时继续使用 FromFunc/FromProcedure。

WithInput 始终配置 T 的 codec，框架构造 RequestOf[T]：

```go
httpv3.Post("/users/{id}/profile", updateProfile,
    httpv3.WithInput(httpv3.FormInput[*ProfileForm]()),
)
httpv3.Post("/users/{id}/attachments", upload,
    httpv3.WithInput(httpv3.MultipartInput[*UploadForm]()),
)
httpv3.Post("/documents", createDocument,
    httpv3.WithInput(httpv3.XMLInput[*Document]()),
)
```

TextInput、StrictJSONInput、MultipartStreamInput、自定义 CustomInput 和 DecodeWith 也可以用于 Data。自定义 decoder 返回 T，框架提供请求视图；无需 DecodeRequest 包装。codec 类型不匹配在构造/注册时报告错误。组级 codec 也面向 T，只适用于相同 Data 类型，路由配置覆盖组配置。

## 显式来源绑定

```go
type SearchParams struct {
    Page int `query:"page"`
    Tag string `query:"tag"`
}
func search(ctx context.Context, req httpv3.RequestOf[SearchParams]) ([]User, error) {
    return service.Search(ctx, req.Data)
}
route := httpv3.Get("/users", search,
    httpv3.WithInput(httpv3.BindInput[SearchParams]()),
)
```

BindInput 支持 path/query/header/cookie 和最多一个 body 标签，包含嵌套字段；未声明来源的字段保持零值，不隐式回退整体 JSON。只有显式启用才会遍历绑定计划和反射赋值。表单/multipart codec 自身的字段绑定仍可能使用反射。注册期反射和类型化构造不等于零请求期反射。

## body、校验与生命周期

默认 JSON 接受无 body；指针 Data 在无体或 JSON null 时保持 nil。空输入 reader 的 EOF 由 codec 判定为错误。RequireBody(codec) 要求至少一个字节，缺失/空体映射 400 并保留 ErrMissingBody，null 不算缺失。字段缺失/null/空值区别由业务字段类型负责。

```go
route := httpv3.Patch("/users/{id}", updateUser,
    httpv3.WithInput(httpv3.RequireBody(httpv3.StrictJSONInput[*UpdateBody]())),
    httpv3.WithDataValidator(validateBody),
    httpv3.WithValidator(validateRequest),
)
```

顺序为解码 Data → WithDataValidator(T) → WithValidator(RequestOf[T]) → handler。nil 指针数据的校验器应自行处理 nil。输入校验错误映射 400；业务授权建议在 middleware 或 handler 判断并返回适当 HTTPError，不将授权混入纯输入校验。

需要延后解码时使用 RequestOf[NoData]，handler 内调用 `ReadBody(ctx, req.RequestInput, codec)`。ReadBody 执行媒体检查并保留输入错误 cause；不自动调用校验器。body 只消费一次，不自动缓存或重放；middleware 已消费 body 时由调用方显式恢复。

端点默认 body 上限 32 MiB；WithBodyLimit 可修改，格式错误为 400、超限为 413、声明媒体不匹配为 415。省略 Content-Type 目前允许。MultipartInput 有独立总大小/内存限制；临时文件默认在端点完成时清理。handler 负责打开/关闭文件及 multipart part。请求视图、上传文件、流式 reader 不能保存给请求结束后的异步任务，需要复制数据。

## 中间件和输出

Middleware 保持 `func(Handler) Handler`，Handler 为 `func(context.Context, *Request, *Response) error`，不依赖各路由的 T。全局、组、路由中间件在输入解码前执行，可拒绝请求而不消费 body；它们包裹执行器及响应编码。进入 handler 前依次执行通用限制、协议检查、解码及两类校验。未提交的错误由外层统一错误管线渲染。

普通 O 默认 JSON；Reply[O] 表达状态/Header/Cookie，FileReply/StreamReply/RedirectReply 支持文件、普通流和重定向。FromAction 接收统一 RequestOf[T] 并默认 204；FromFunc/FromProcedure 是无输入适配器。Raw 负责自行读写响应，保留中间件、body 上限和清理，不启用 DTO 解码。SSE/WebSocket/连接升级不在当前 v3 承诺范围内，不新增虚构入口。

OpenAPI 导出 Data schema，显式绑定导出来源参数；WithParameter 只声明按需参数文档，不触发解析或必填校验。动态 decoder 和不能准确推断的协议标记未知。批量注册预检失败不安装，底层安装失败可能保留部分路由，不提供事务回滚。

## 迁移与验证

相对 v2 的破坏性区别：方法构造器从任意 I 改为统一 RequestOf[T]，WithInput 从整个输入改为 Data codec，来源自动绑定必须显式 BindInput。v2 包保留原有 API，不修改其行为。迁移时去掉 `WithInput(DecodeRequest(codec))` 的外层包装，直接 `WithInput(codec)`。

验证：`go test ./ghttp/v3`、`go test -race ./ghttp/v3`、`make check PKGS=./ghttp/v3`。当前没有同口径 Gin/Echo 性能结果，不承诺性能持平。

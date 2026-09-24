# ghttp v2

[English](README.en.md) · [设计与阶段边界](DESIGN.md)

可运行的完整示例：[HTTP 示例与 curl 请求](examples/http/README.md)，源码见 [main.go](examples/http/main.go)。

实验性、pre-v1，禁止直接用于生产开发。v2 是独立包，不兼容替换 v1。当前实现供 API review，尚未承诺 API 稳定。

## 门面

```go
import httpv2 "github.com/sofiworker/gk/ghttp/v2"

type UpdateInput struct {
    Username string `path:"username"`
    Body UpdateBody `body:"json"`
}

func update(ctx context.Context, in *UpdateInput) (*User, error) {
    return service.Update(ctx, in.Username, in.Body)
}

route := httpv2.Patch("/users/{username}", update)
if err := route.Mount(api); err != nil {
    return err
}
```

支持 Get、Head、Post、Put、Patch、Delete、Options、Method。handler 的 I/O 类型自动推断，值和指针四种组合均可使用；根输入不支持多级指针。无输入、仅 error 返回使用显式适配器，避免任意函数签名的反射调用。

Route.Err 在注册时报告配置错误；Route.Serve 用于执行而不匹配路径。Mount 复用现有 Server/Group 的路由器、中间件与错误处理，不重新实现路由树。HEAD 执行端点但抑制响应体。

## 输入与输出

不需要业务 DTO、只需读取请求来源时，可直接接收 `RequestInput`，其 `Sources()` 返回零复制的来源视图：`Path`、`QueryFirst`/`QueryValues`、`Header` 和 `Cookie`。它适合轻量端点；需要保留领域输入类型时仍使用自动绑定 DTO。

```go
httpv2.Get("/health/{name}", func(ctx context.Context, in *httpv2.RequestInput) (Health, error) {
    src := in.Sources()
    return check(ctx, src.Path("name"), src.Header("X-Trace"))
})
```

无来源标签默认整体 JSON；使用标签时仅绑定声明的来源。path/query/header/cookie 字段与一个 body 字段可组合，body 支持 json/xml/form/text。标签计划在注册时解析；运行时仍通过反射访问字段，不能称为零反射。

```go
httpv2.Post("/documents", createDocument,
    httpv2.WithInput(httpv2.XMLInput[*Document]()),
    httpv2.WithOutput(httpv2.XMLOutput[*Document]()),
)
```

显式 WithInput 替换整个默认输入契约，不叠加标签绑定。codec 的泛型类型必须与 handler 一致，不匹配在注册时报错。Content-Type 存在且与输入 codec 不匹配时返回 415；当前允许省略 Content-Type。解析错误为 400，请求体超限为 413。默认端点请求体上限 32 MiB，可通过 WithBodyLimit 设置。

JSONOutput、XMLOutput、TextOutput、HTMLOutput、EmptyOutput 提供基本输出。HTML 输出只接收已经渲染的内容，调用方应使用 html/template 或显式转义。JSON nil 输出表示 null，不暗中变成 204。CustomInput/CustomOutput 通过 Decoder/Encoder 小接口扩展。

Reply[T] 用于状态码、Header、Cookie 和响应体；默认 JSON 输出自动展开 Reply。自定义响应体编码使用 ReplyOutput(bodyOutput)，不把元数据序列化进 body。

## 文件输入

```go
type Upload struct {
    Title string `form:"title"`
    File *multipart.FileHeader `form:"file"`
    Files []*multipart.FileHeader `form:"files"`
}

httpv2.Post("/attachments", upload,
    httpv2.WithInput(httpv2.MultipartInput[*Upload]()),
)
```

MultipartInput 默认限制整个 multipart 请求体 32 MiB、文件内存驻留 1 MiB，超出内存部分写入临时文件；可传 MultipartLimits 覆盖。端点 WithBodyLimit 和 multipart 自身限制均生效，较小者优先。

handler 自行 Open/Close 文件，框架在端点返回（包括错误和 panic 展开）后清理临时文件。不能把 FileHeader 留给请求结束后的异步任务；需要长期持有时在 handler 内复制。单文件字段收到多个文件会报错；文件缺失为 nil，由业务校验。流式上传使用 MultipartStreamInput，见下文。

## 性能与 review 边界

禁止代码生成，核心 handler 和显式适配器不使用 reflect.Call。来源标签计划在注册时构建；表单与 multipart 绑定按注册期计划执行，字段赋值及 JSON/XML 标准库 codec 仍可能使用反射。无输入端点基准包含 httptest recorder 开销，不代表裸框架吞吐。

已提供显式 Accept 协商、类型化 validator、嵌套来源标签和流式上传。批量安装不提供事务回滚；presence 使用指针区分缺失，不提供自动业务校验标签 DSL。普通 FormInput 只支持 URL 编码表单，multipart 必须显式使用 MultipartInput。

验证：`go test ./ghttp/v2`、`go test -race ./ghttp/v2`、`go test ./ghttp/v2 -run '^$' -bench . -benchmem`。

## 无输入、无输出与响应元数据

```go
health := httpv2.FromFunc(http.MethodGet, "/health", healthHandler)
remove := httpv2.FromAction(http.MethodDelete, "/users/{id}", deleteHandler)
logout := httpv2.FromProcedure(http.MethodPost, "/logout", logoutHandler)
```

对应签名分别为 `func(context.Context) (O, error)`、`func(context.Context, I) error` 和 `func(context.Context) error`。后两种默认 204，可用 WithOutput 显式覆盖为空 JSON 对象等协议。

```go
func signIn(ctx context.Context, in *Credentials) (httpv2.Reply[*Session], error) {
    session, err := authenticate(ctx, in)
    if err != nil {
        return httpv2.Reply[*Session]{}, err
    }
    return httpv2.Reply[*Session]{
        Body: session,
        Cookies: []*http.Cookie{{Name: "session", Value: session.Token, HttpOnly: true}},
    }, nil
}

route := httpv2.Post("/signin", signIn)
```

建议先阅读 [REVIEW.md](REVIEW.md) 按文件审查职责和未完成项。

## Group 注册

Group 只提供注册期的前缀与默认配置，仍复用原 ghttp Server/Group 匹配器：

```go
api := v2.New()
users := api.Group("/users", v2.GroupInput(v2.JSONInput[*UserInput]()),
    v2.GroupOutput(v2.JSONOutput[*UserView]()),
).Use(authenticate)

get, err := v2.GetIn(users, server, "/{username}", getUser)
if err != nil { return err }

admin := users.Group("/admin").Use(requireAdmin)
_, err = v2.DeleteIn(admin, server, "/{username}", deleteUser)
```

支持父子前缀拼接与 middleware 父到子执行。组级输入/输出 codec 通过 `GroupInput[T]`/`GroupOutput[T]` 声明，只能继承到类型相同的 handler；类型不匹配在构造路由时返回错误。路由级 `WithInput`/`WithOutput` 后应用并覆盖同类型组默认。`Routes(group,target,routes...)` 可给已有 route 加前缀并逐条挂载；批量挂载不是事务操作，失败前已成功的路由不会回滚。

## 推荐：绑定目标后集中 Register

```go
api := v2.New(server).With(v2.WithBodyLimit(8 << 20))
users := api.Group("/users").Use(authenticate)

return users.Register(
    v2.Get("/{username}", getUser),
    v2.Post("", createUser),
    v2.Patch("/{username}", updateUser),
)
```

Root/Group 的 With 接收同一套 Option。Register 按根→父组→子组→路由的顺序合并配置，再生成类型化执行器；middleware 依序追加，codec 和大小限制后者覆盖前者。已注册路由不受后续组配置变化影响。同一 Route 定义可重复用于不同组。

Register 先完成本批次的类型配置检查与同方法同路径查重，再逐条挂载；底层路由器的路径冲突或冻结错误仍可能导致部分安装，不承诺事务回滚。错误包含方法及组内完整路径。

Group.Mount/Routes 现在也应用全部组配置，不再仅处理前缀和 middleware，这是实验性 v2 的行为变化。旧 GetIn 等入口继续可用。路由构造保留独立 Serve/Err 的兼容检查，Register 会再次按组配置编译；没有请求期反射调用，注册期额外成本尚待重新测量。

## 输入执行计划与手写 decoder

执行器在注册期选择 `decode(ctx, req) (I, error)`，请求时直接把返回的 I 传给 handler。自动 decoder 仍保持原有 T/*T 行为；闭包只捕获规则，每次请求构造独立输入。path/query/header/cookie 的标量转换器在注册期选择，不支持的字段类型提前报错；字段赋值仍使用反射。

热点接口可选择 `WithInput(DecodeWith(decodeUser))`，其中 decodeUser 签名为 `func(context.Context, *Request) (*UserInput, error)`。该函数应为每次请求返回独立输入，框架不替它修正 nil 或复用对象。请求体上限及错误归类仍由外层执行器处理。DecodeWith 不声明 Content-Type，协议校验需要由该自定义函数负责。

需要由解码器自行负责 body 大小限制、Content-Type 校验和 multipart 临时文件清理时，可再配置 `WithUnsafeFastPath()`，让注册期生成的执行器跳过这些通用防护路径。默认不启用；只应在外层已完成相同约束或请求不包含 body 时使用。这里的 Unsafe 指调用方承担协议与资源验证责任，不涉及 Go `unsafe` 包。

原 Decoder.Decode(req, *I) 扩展继续可用，由注册期适配器转换为新的内部执行形态。当前只优化来源字段转换器及执行器；Form/multipart 字段计划在注册期构造，JSON/XML 标准库反射未消除，也未承诺零反射。

DecodeWith 中读取少量 query 字段推荐使用 `req.QueryFirst("page")`（返回值及是否存在），多值使用 `req.QueryValues("tag")`。这些方法复用原 ghttp 的惰性解析，不物化完整 map；大量参数会回退缓存映射。需要完整映射时继续使用 req.Query。不要跨请求缓存返回值。

RequestInput/Sources 及其底层池化请求只在当前请求处理期间有效，不应保存供异步任务使用。值类型 RequestInput 在注册期选择直接构造函数，避免通用 decoder 临时目标对象；Sources 本身不增加 query 索引，仍复用原有按需解析策略。

## 普通 HTTP 主线入口

v2 按后续主线开发，不提供 v1 API 兼容层。`NewServer` 提供独立监听与路由入口，内部复用已有 HTTP 内核：

```go
server := httpv2.NewServer(httpv2.WithReadHeaderTimeout(5 * time.Second))
api := server.Group("/api")
err := api.Register(httpv2.Get("/health", func(ctx context.Context, in httpv2.RequestInput) (string, error) {
    return in.HeaderValue("X-Trace"), nil
}))
// 检查 err 后调用 server.Run(":8080")；退出时调用 server.Shutdown(ctx)。
```

支持 TLS、外部 listener、全局/组/路由 middleware、HEAD/OPTIONS/405。启动前完成配置和注册。`Register` 预检配置错误；底层安装中发生冲突时，之前成功安装的路由仍保留，不承诺事务回滚。

## 校验与内容协商

`WithValidator(func(context.Context, T) error)` 在解码后、handler 前执行；错误保留 cause，并归类为无效输入。组可设置默认 validator，路由覆盖。validator 类型必须与 handler 输入一致。

`WithInput(StrictJSONInput[T]())` 拒绝未知字段及多个 JSON 值；不改变标准 JSON 重复键语义。来源标量指针可区分未提供与已提供空值；字段转换失败不会调用 handler。

`WithNegotiation(JSONOutput[T](), XMLOutput[T]())` 根据 Accept 权重、通配符和媒体参数选择输出，并添加 `Vary: Accept`；未提供 Accept 使用首项，没有可接受表示返回 406。默认不启用协商，显式 `WithOutput` 行为保持固定表示。

## 文件、流和重定向

`FileReply` 接收当前原始 `*http.Request` 与 `io.ReadSeeker`，通过标准库实现 Range、Last-Modified 条件请求及 HEAD。`DownloadName` 设置附件文件名。`StreamReply` 复制 `io.Reader`，可以指定状态码和 Content-Type；传入当前 Request 可使 HEAD 不消费流。`RedirectReply` 设置 Location 和合法重定向状态（默认 302）。这些值直接作为 handler 返回值，不经 JSON 序列化。

文件/流需要关闭时同时设置 `Closer`，框架在编码完成（包括错误和 HEAD）后关闭。**不要在 handler 内 defer Close**，否则返回后编码开始前资源已关闭。未提供 Closer 时资源由调用方管理。ServeContent 的错误由标准库生成 HTTP 响应，部分写出后无法回退响应。

## 流式 multipart 上传

使用 `WithInput(MultipartStreamInput())`，handler 接收 `*multipart.Reader`，通过 `NextPart()` 逐段读取，读取完成关闭 part。该模式不落整份临时文件、不预先缓存整个上传，端点 `WithBodyLimit` 仍然生效。手工解析的格式错误需要 handler 返回适当的 HTTP 错误，大小限制错误由执行器识别为 413。普通 MultipartInput、DecodeWith 和 RequestInput 路径的临时文件默认在端点结束时清理，包括错误和 panic 展开。

## 显式转移验证责任

`WithoutBodyLimit()`、`WithoutContentTypeCheck()`、`WithoutMultipartCleanup()` 分别转移端点大小限制、媒体类型检查和临时文件清理责任；codec 自身约束仍生效。`WithUnsafeFastPath()` 是三者的组合，不使用 Go unsafe。后续 `WithBodyLimit(n)` 可重新开启端点大小限制。性能对照必须分别列出默认与关闭检查的结果，不能视为等价工作量。

## 注册元数据与 OpenAPI

`server.OpenAPI("服务名", "版本")` 显式导出已成功注册路由的 OpenAPI 3.1 JSON，包含组完整路径、来源参数、可推断的请求/响应类型及静态状态码。动态解码、无法准确描述的 XML/multipart 或协商输出以 `x-gk-*-schema-unavailable` 标记，不猜测契约。生成后可作为普通响应提供给文档工具。OpenAPI 导出不是完整业务 schema/校验规则生成器。

`HTTPError{Status: 400, Cause: err}` 可用于手动解析失败，保留 errors.Is/As 错误链；无效错误状态转为 500。`Recovery()` 可加入全局 middleware，把 panic 交给统一错误管线。生命周期与功能边界见 [HTTP_MAINLINE.md](HTTP_MAINLINE.md)。

## 直接请求与响应 handler

```go
route := v2.Raw(http.MethodPost, "/ss",
    func(ctx context.Context, req *v2.Request, resp *v2.Response) error {
        resp.Header().Set("Content-Type", "text/plain; charset=utf-8")
        _, err := resp.Write([]byte("hello"))
        return err
    },
)
```

Raw 返回普通 Route，可由 Server/Group.Register 注册。handler 返回 error，成功返回 nil；没有自动输出编码，不会追加 JSON 或自动 204。未提交响应的错误交给统一错误处理，已写出响应不追加错误体。Group 的 BodyLimit、中间件及默认 multipart 清理仍生效，HEAD 抑制响应体。组级输入输出 codec、validator 和协商不适用；Raw 路由显式配置这些选项会注册失败。输入协议校验由 handler 负责。

# ghttp v2 门面设计

> 状态：实验性设计与分阶段实现。v2 不承诺兼容 v1；当前阶段不改变 v1 API。

## 目标

v2 先解决 API 使用形态，不重新设计路由树、匹配算法或 middleware 执行内核。用户只需要表达：HTTP 方法、输入来源、业务 handler 和输出协议。方法、输入形态、输出形态不再通过 `GetParams`、`PatchParamsBody` 等组合函数展开。

## 核心 handler 契约

业务端点的核心形状是：

```go
type Endpoint[I, O any] func(context.Context, I) (O, error)
type Action[I any] func(context.Context, I) error
type Procedure func(context.Context) error
```

`I`、`O` 可以独立为值或指针，以下组合全部是合法目标：

```go
func(context.Context, Input) (Output, error)
func(context.Context, *Input) (Output, error)
func(context.Context, Input) (*Output, error)
func(context.Context, *Input) (*Output, error)
```

框架不得因为顶层输入是指针而改变绑定契约，也不得要求用户用匿名函数转发参数。

## 门面原则

HTTP 方法只表达 HTTP 语义：

```go
api.Get(path, handler)
api.Post(path, handler)
api.Put(path, handler)
api.Patch(path, handler)
api.Delete(path, handler)
api.Head(path, handler)
api.Options(path, handler)
api.Method(method, path, handler)
```

公共 API 不再组合方法和输入形态。`GetParams`、`PostBody`、`PatchParamsBody`、`DeleteNone` 等仅作为 v1 兼容实现存在，不成为 v2 门面。

最常见的 JSON API 应接近：

```go
api.Get("/users/{username}", s.GetUser)
api.Post("/users", s.CreateUser)
api.Patch("/users/{username}", s.UpdateUser)
api.Delete("/users/{username}", s.DeleteUser)
```

## 输入协议

输入描述与 HTTP 方法正交。默认输入由 handler 类型和字段标签推断；需要覆盖时通过选项指定：

```go
type UpdateUserInput struct {
	Username string         `path:"username"`
	Body     UpdateUserBody `body:"json"`
}

api.Patch("/users/{username}", s.UpdateUser,
	ghttp.WithInput(ghttp.JSONInput[UpdateUserInput]()))
```

第一阶段的来源模型：`path`、`query`、`header`、`cookie` 由结构体标签绑定；请求体由 `body:"json"`、`body:"form"`、`body:"xml"` 等声明绑定。一个输入类型可以组合多个来源，但最多一个请求体来源。

框架内置输入构造器：

```go
ghttp.JSONInput[T]()
ghttp.FormInput[T]()
ghttp.XMLInput[T]()
ghttp.TextInput[T]()
```

自定义输入通过小接口扩展，不要求用户接触反射：

```go
type Decoder[T any] interface {
	Decode(*Request, *T) error
}
```

## 输出协议

普通端点默认 JSON 输出，不要求重复写 `JSON[T]()`。特殊协议通过选项覆盖：

```go
api.Post("/documents", s.CreateDocument,
	ghttp.WithInput(ghttp.XMLInput[Document]()),
	ghttp.WithOutput(ghttp.XMLOutput[DocumentResponse]()))
```

内置输出构造器：

```go
ghttp.JSONOutput[T]()
ghttp.XMLOutput[T]()
ghttp.TextOutput[T]()
ghttp.HTMLOutput[T]()
ghttp.EmptyOutput()
```

需要状态码、Header 或 Cookie 时使用统一响应包装，而不是逃逸到 raw handler：

```go
type Reply[T any] struct {
	Body    T
	Status  int
	Headers http.Header
	Cookies []*http.Cookie
}
```

普通 `(T, error)` 与 `(Reply[T], error)` 都应支持值和指针 `T`。空响应必须有明确契约，不以 `nil` 静默改变为 204。

## 自定义点与框架主线

框架主线负责输入绑定计划、Content-Type/Accept、默认 JSON 编解码、错误转换、OpenAPI 描述和 middleware 衔接。用户可自定义 decoder、encoder 和响应元数据；自定义扩展不得要求导出万能 Context。

## 反射与性能边界

禁止代码生成。允许注册阶段反射：校验 handler 签名、解析类型和标签、构建绑定计划、生成 schema。请求阶段不得重复解析类型、标签或 codec；绑定计划和编码器必须缓存为不可变执行计划。

核心 `(context.Context, I) (O, error)` 端点通过泛型适配到内部执行器，避免每请求 `reflect.Call`。`FromFunc`/`FromAction`/`FromProcedure` 是显式适配器，通过普通闭包复用泛型执行器，不使用 reflect.Call。任意函数签名不在本阶段承诺范围内。

## 分阶段实现

1. 固化 `Endpoint`、`Action`、`Procedure`、`Reply` 和输入/输出 facade 的公开契约。
2. 实现值/指针四种组合的注册与执行测试。
3. 实现 JSON、Form、XML、Text、HTML 的输入/输出 facade 和自定义 codec。
4. 增加统一方法入口与选项，复用现有绑定计划和 middleware 内核。
5. 测量注册期反射、请求期分配和基准性能，再决定是否增加更多适配器。

每阶段保持 v1 文件和行为不变；v2 的破坏性调整只在 `ghttp/v2` 内发生。

## 当前实施状态

详细实现范围以 README.md 与 REVIEW.md 为准。第一阶段使用兼容 Go 1.25 的包级泛型方法构造器，而非泛型方法或 In.JSON 命名空间。已实现来源绑定、文件上传、Reply 元数据、有限签名适配和单条 Route.Mount；复用 v1 路由器和中间件，不生成代码。

普通 HTTP 主线进度以 HTTP_MAINLINE.md 为准。已扩展独立 Server、嵌套绑定、validator、显式内容协商、流式上传、文件/流式输出；OpenAPI 基于注册元数据导出。批量 Mount 不承诺安装失败回滚。禁止将类型计划称为零运行时反射。HTML 输出为已渲染内容，模板转义由调用方负责。

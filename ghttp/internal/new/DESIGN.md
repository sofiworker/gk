# new 路由与 Handler 设计草案

## 1. 目标

`ghttp/internal/new` 用于验证一种面向 Go 泛型的 HTTP 路由模型：

```text
显式声明 endpoint
    -> 静态保存路径、参数和输入输出契约
    -> 注册阶段生成 compiled handler
    -> Gin 风格 radix tree 完成路径匹配
    -> 运行时只执行预编译的提取器、业务函数和输出编码器
```

目标不是复制 Gin 的全部 API，而是保留 Gin 的高效前缀树，同时让输入解析和输出编码具有明确的类型与契约。

## 2. API 方向

推荐的链式入口：

```go
mux.Post("/users/{id}").
	Path[Int64Path]("id").
	Query[UserQuery]("verbose").
	Body[CreateUser](JSON()).
	Output(JSON[UserOutput]().Status(http.StatusCreated)).
	Handler(func(
		ctx context.Context,
		input RequestInput[Int64Path, UserQuery, CreateUser],
	) (UserOutput, error) {
		return UserOutput{}, nil
	})
```

原始 HTTP handler 使用单独入口：

```go
mux.Get("/debug").RawHandler(func(
	ctx context.Context,
	req *Request,
	resp *Response,
) error {
		resp.WriteHeader(http.StatusNoContent)
		return nil
	})
```

业务 handler 不直接负责 JSON、XML、状态码或响应头；这些信息必须由 `Input`、`Output` 和 `Response` 契约显式声明。

## 3. Handler 泛型模型

业务函数使用输入和输出泛型：

```go
type HandlerFunc[In, Out any] func(context.Context, In) (Out, error)
```

它只描述业务计算，不描述 HTTP 编码。

```go
type Handler[In, Out any] struct {
	method string
	path   string

	input  InputSpec[In]
	output OutputSpec[Out]
	h      HandlerFunc[In, Out]

	// 注册阶段生成，运行时直接调用。
	execute func(context.Context, *Request, *Response) error
}
```

不同 `Handler[In, Out]` 不能直接放入同一棵树，因为 Go 泛型实例化后是不同的具体类型。树中保存类型擦除后的接口：

```go
type compiledHandler interface {
	Serve(context.Context, *Request, *Response) error
}
```

注册时将泛型 `Handler[In, Out]` 编译为 `compiledHandler`。请求匹配完成后不再反射判断 handler 类型。

## 4. 输入契约

输入契约负责从请求中构造业务输入：

```go
type InputSpec[T any] interface {
	Decode(*Request) (T, error)
}
```

请求体格式不绑定到 `T`，而由 codec 决定：

```go
Body[CreateUser](JSON())
Body[CreateUser](XML())
Body[CreateUser](Form())
```

推荐的输入来源包括：

```go
PathInt64("id")
PathString("name")
QueryBool("verbose")
QueryString("locale")
HeaderString("X-Tenant")
CookieString("session")
Body[CreateUser](JSON())
Body[CreateUser](XML())
Form[CreateUser]()
Multipart[UploadForm]()
RawRequest()
```

## 5. 输入组合

路径、query 和 body 不是业务结构体的字段，因此不要求把它们写入 `CreateUser`。组合输入使用独立的框架输入容器：

```go
type NoPath struct{}
type NoQuery struct{}
type NoBody struct{}

type RequestInput[P, Q, B any] struct {
	Path  P
	Query Q
	Body  B
}
```

没有某一来源时使用零尺寸占位类型：

```go
RequestInput[NoPath, NoQuery, CreateUser]
RequestInput[UserPath, UserQuery, NoBody]
RequestInput[NoPath, UserQuery, CreateUser]
```

占位类型不承载数据，运行时不产生有效负载。

### 5.1 原始请求

原始 HTTP 请求不放进 path、query 或 body 类型中，而是始终由 `Request` 提供：

```go
type Request struct {
	*http.Request
	Params Params
}
```

需要原始请求的业务 handler 可以使用扩展 handler 形态：

```go
func(ctx context.Context, req *Request, input UserInput) (UserOutput, error)
```

或者使用 `RawHandler` 完全接管 HTTP 响应。

## 6. 输出契约

`(Out, error)` 不默认代表 JSON，也不默认代表状态码 200。输出格式和状态码必须显式声明：

```go
type OutputSpec[T any] interface {
	Encode(*Response, T) error
}
```

示例：

```go
Output(JSON[UserOutput]())
Output(XML[UserOutput]())
Output(Text())
Output(Bytes())
Output(HTML())
Output(Stream())
Output(NoContent())
Output(Redirect())
```

状态码属于输出契约：

```go
Output(JSON[UserOutput]().Status(http.StatusCreated))
```

响应头、Cookie 和 Content-Type 也由 `OutputSpec` 或 `Response` 明确处理。

完全手写响应时使用：

```go
type Response struct {
	http.ResponseWriter
}
```

```go
RawHandler(func(ctx context.Context, req *Request, resp *Response) error {
	resp.Header().Set("X-Trace", "enabled")
	resp.WriteHeader(http.StatusNoContent)
	return nil
})
```

如果 endpoint 没有显式 `Output`，注册阶段返回错误，而不是隐式选择 JSON。

## 7. 编译流程

一个 typed endpoint 的注册过程：

```text
Get/Post(method, path)
    -> 解析路径模板
    -> 校验参数名称、重复参数和 catch-all 位置
    -> 记录 Path/Query/Body/Header 等 InputSpec
    -> 校验 Handler 输入类型与 InputSpec 组合
    -> 校验 OutputSpec 与 Handler 输出类型
    -> 生成 compiledHandler
    -> 按 method 和 path 插入 radix tree
```

编译后的执行器逻辑等价于：

```go
func execute(ctx context.Context, req *Request, resp *Response) error {
	input, err := inputSpec.Decode(req)
	if err != nil {
		return err
	}

	output, err := businessHandler(ctx, input)
	if err != nil {
		return err
	}

	return outputSpec.Encode(resp, output)
}
```

实际实现应在注册阶段固定参数位置、解析器和编码器，避免请求期间反射扫描字段或重新解析路由描述。

## 8. 路由树

节点使用 Gin 风格的压缩 radix tree：

```go
type Node struct {
	path      string
	indices   string
	children  []*Node
	priority  uint32
	nType     nodeType
	maxParams uint8
	wildChild bool
	fullPath  string
	handler   compiledHandler
}
```

匹配优先级：

```text
静态节点 > 参数节点 > catch-all 节点
```

支持：

```text
/users/new
/users/:id
/files/*path
/users/{id}
/files/{path...}
```

同一 method 和 path 不允许重复注册。不同 method 使用不同的根树或 method-aware 根节点。

## 9. 错误处理

注册阶段错误包括：

- 路径不是 `/` 开头；
- 参数名称非法；
- 参数名称重复；
- catch-all 不是最后一段；
- 静态路径和动态路径冲突；
- 同 method、同 path 重复注册；
- Handler 输入和 InputSpec 不匹配；
- Handler 输出和 OutputSpec 不匹配；
- endpoint 未声明 Output。

请求阶段错误包括：

- 路由不存在：404；
- 路径存在但 method 不匹配：405，并设置 `Allow`；
- 参数解析失败：400 或统一输入错误；
- body 格式不支持：415；
- 输出编码失败：交给统一错误处理链；
- handler 返回业务错误：交给统一错误处理链。

## 10. 性能目标

树匹配目标是达到 Gin 同等级别：

- 静态路径优先；
- 压缩 radix tree；
- 参数节点和 catch-all 节点专门化；
- 不在热路径重新解析路由模板；
- 尽量避免 `map[string]string` 参数容器；
- 参数位置在注册阶段固定。

相比 Gin，潜在优化点主要在 typed input/output：

- 不使用反射扫描业务结构体；
- 不在请求期间解析 tag；
- 不通过 `[]any` 传递参数；
- 不进行动态函数参数拆箱；
- body decoder 和 output encoder 在注册阶段固定；
- `compiledHandler` 只做一次接口分派。

不能假设一定超过 Gin，必须通过基准测试验证：

```text
静态路由
单 path 参数
多 path 参数
query 参数
JSON body
XML body
Form body
原始 http.Request
完整输入解析和输出编码
```

指标至少包括：

```text
ns/op
allocs/op
B/op
```

## 11. 不采用的方案

### 11.1 `[]any` 参数数组

不采用：

```go
func(context.Context, []any) (any, error)
```

原因：失去静态类型检查，产生装箱、切片和类型断言开销。

### 11.2 运行时反射调用

不采用 `reflect.Value.Call` 作为热路径调用方式。反射可以作为原型或注册阶段校验工具，但不能作为最终 handler 执行路径。

### 11.3 隐式 JSON 输出

不把 `(Out, error)` 默认编码为 JSON，也不默认状态码 200。输出契约必须显式声明。

### 11.4 自动代码织入

Go `go build` 不会自动执行代码生成，也没有 AspectJ 风格的标准编译期织入机制。第一版不依赖自定义编译器或隐式 CLI。

## 12. 当前推荐落地顺序

1. 完成 Gin 风格 radix tree 和 method-aware 匹配。
2. 定义 `Request`、`Response`、`Params`。
3. 定义 `InputSpec[T]` 和 `OutputSpec[T]`。
4. 实现 `PathInt64`、`QueryBool`、`JSONBody[T]`、`XMLBody[T]`、`FormBody[T]`。
5. 实现 `compiledHandler`，注册阶段固定 extractor 和 encoder。
6. 增加 raw HTTP handler。
7. 增加注册冲突、输入错误、输出契约和 method 行为测试。
8. 增加与 Gin 的基准测试。

这份设计暂不承诺兼容现有 `ghttp` API，也不把新包接入现有 `Server`。

# Result接口重设计文档

## 1. 设计目标

### 1.1 核心目标
1. Result能够检测是否是handler链的最后一个
2. 支持Spring Boot式的"返回值自动marshal"模式
3. 保持向后兼容性
4. 零性能开销（hot path不引入额外分支）

### 1.2 非目标
- 不改变现有Result接口的语义
- 不引入额外的抽象层（保持简单）

## 2. 设计方案

### 2.1 Result接口扩展

#### 方案A：扩展Result接口（推荐）

```go
// Result接口保持不变，保持向后兼容
type Result interface {
    Execute(ctx *Context)
}

// 新增：ResultWithIsLast接口，支持isLast参数
type ResultWithIsLast interface {
    Execute(ctx *Context, isLast bool)
}

// 新增：Spring Boot风格的自动marshal Result
type AutoResult struct {
    data     interface{}
    code     int
    headers  map[string]string
}

func (r *AutoResult) Execute(ctx *Context, isLast bool) {
    if !isLast {
        // 不是最后一个handler，不执行响应
        // 只有当是最后一个handler时才写入响应
        return
    }

    // 设置自定义headers
    for k, v := range r.headers {
        ctx.Header(k, v)
    }

    // 自动marshal返回值
    r.autoMarshal(ctx)
}

func (r *AutoResult) autoMarshal(ctx *Context) {
    contentType := ctx.GetHeader("Accept")
    if contentType == "" {
        contentType = "application/json"
    }

    // 根据Accept header选择编码方式
    switch {
    case strings.Contains(contentType, "application/json"):
        ctx.JSON(r.code, r.data)
    case strings.Contains(contentType, "application/xml"):
        ctx.XML(r.code, r.data)
    default:
        // 默认JSON
        ctx.JSON(r.code, r.data)
    }
}

// 便捷函数
func Auto(data interface{}) Result {
    return &AutoResult{data: data, code: http.StatusOK}
}

func AutoCode(data interface{}, code int) Result {
    return &AutoResult{data: data, code: code}
}

func AutoWithHeaders(data interface{}, code int, headers map[string]string) Result {
    return &AutoResult{data: data, code: code, headers: headers}
}
```

#### 方案B：保持Result接口不变，在Context中处理（备选）

```go
// 保持Result接口不变
type Result interface {
    Execute(ctx *Context)
}

// 在Context的Next()中添加特殊处理
func (c *Context) Next() {
    c.handlerIndex++

    // 检查是否是最后一个handler
    isLast := c.handlerIndex >= len(c.handlers)-1

    for c.handlerIndex < len(c.handlers) {
        if c.handlers[c.handlerIndex] != nil {
            handler := c.handlers[c.handlerIndex]

            // 如果是ResultHandler，需要特殊处理
            if resultHandler, ok := handler.(resultHandlerWrapper); ok {
                result := resultHandler.handler(c)
                if result != nil {
                    if resultWithIsLast, ok := result.(ResultWithIsLast); ok {
                        resultWithIsLast.Execute(c, isLast)
                    } else {
                        // 向后兼容：普通Result只在最后一个执行
                        if isLast {
                            result.Execute(c)
                        }
                    }
                }
            } else {
                // 普通HandlerFunc
                handler(c)
            }
        }
        c.handlerIndex++
        isLast = c.handlerIndex >= len(c.handlers)-1
    }
}

type resultHandlerWrapper struct {
    handler ResultHandler
}

func (w resultHandlerWrapper) Handle(ctx *Context) {
    // 这个方法不会被调用，因为我们在Next()中特殊处理
}
```

**决策：采用方案A** - 扩展Result接口更简单，向后兼容性更好。

### 2.2 Context修改

```go
// Context结构体添加辅助方法
type Context struct {
    // ...现有字段...
    handlers     []HandlerFunc
    handlerIndex int
}

// 新增：检查当前是否是最后一个handler
func (c *Context) IsLastHandler() bool {
    return c.handlerIndex >= len(c.handlers)-1
}

// 修改：Next()方法支持ResultWithIsLast
func (c *Context) Next() {
    c.handlerIndex++

    for c.handlerIndex < len(c.handlers) {
        if c.handlers[c.handlerIndex] != nil {
            handler := c.handlers[c.handlerIndex]

            // 检查是否是ResultHandlerWrapper
            if wrapper, ok := handler.(*resultHandlerWrapper); ok {
                result := wrapper.handler(c)
                if result != nil {
                    isLast := c.handlerIndex >= len(c.handlers)-1

                    // 优先执行ResultWithIsLast
                    if resultWithIsLast, ok := result.(ResultWithIsLast); ok {
                        resultWithIsLast.Execute(c, isLast)
                    } else {
                        // 向后兼容：普通Result只在最后一个执行
                        if isLast {
                            result.Execute(c)
                        }
                    }

                    // Result执行后，是否继续？
                    // Spring Boot风格：Result执行后应该停止链
                    // 保持向后兼容：Result不影响链的执行
                    // 决策：保持向后兼容，Result执行后继续
                }
            } else {
                // 普通HandlerFunc
                handler(c)
            }
        }
        c.handlerIndex++
    }
}

// resultHandlerWrapper 包装ResultHandler为HandlerFunc
type resultHandlerWrapper struct {
    handler ResultHandler
}

func (w *resultHandlerWrapper) Handle(ctx *Context) {
    // 这个方法永远不会被调用，因为我们在Next()中特殊处理
    panic("resultHandlerWrapper.Handle should not be called")
}

// 修改：Wrap函数返回resultHandlerWrapper
func Wrap(handler ResultHandler) HandlerFunc {
    return &resultHandlerWrapper{handler: handler}
}
```

### 2.3 Spring Boot风格使用示例

#### 示例1：直接返回结构体

```go
// 定义响应结构体
type UserResponse struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

// Handler直接返回Result（像Spring Boot）
func getUser(ctx *Context) Result {
    user := UserResponse{
        ID:    1,
        Name:  "John",
        Email: "john@example.com",
    }
    return Auto(user)
}

// 注册路由
server.GET("/users/:id", Wrap(getUser))
```

#### 示例2：带状态码

```go
func createUser(ctx *Context) Result {
    user := UserResponse{
        ID:    2,
        Name:  "Jane",
        Email: "jane@example.com",
    }
    return AutoCode(user, http.StatusCreated)
}

server.POST("/users", Wrap(createUser))
```

#### 示例3：自定义headers

```go
func getCachedUser(ctx *Context) Result {
    user := UserResponse{
        ID:    1,
        Name:  "John",
        Email: "john@example.com",
    }
    headers := map[string]string{
        "Cache-Control": "max-age=3600",
        "X-Cache-Hit":   "true",
    }
    return AutoWithHeaders(user, http.StatusOK, headers)
}

server.GET("/users/:id", Wrap(getCachedUser))
```

#### 示例4：混合使用Result和普通Handler

```go
// 中间件：记录请求日志
func logger(ctx *Context) {
    start := time.Now()
    defer func() {
        ctx.Logger().Infof("%s %s took %s",
            string(ctx.fastCtx.Method()),
            string(ctx.fastCtx.Path()),
            time.Since(start),
        )
    }()
    ctx.Next()
}

// Handler：返回Result
func getUser(ctx *Context) Result {
    user := UserResponse{ID: 1, Name: "John"}
    return Auto(user)
}

// 注册：中间件 + ResultHandler
server.Use(logger)
server.GET("/users/:id", Wrap(getUser))

// 执行流程：
// 1. logger执行
// 2. logger调用Next()
// 3. getUser返回AutoResult
// 4. AutoResult.Execute(ctx, isLast=true) -> 写入响应
// 5. 返回logger的defer
// 6. logger记录日志
```

### 2.4 向后兼容性

#### 现有Result继续工作

```go
// 现有的Result实现
func (r *JsonResult) Execute(c *Context) {
    if c == nil {
        return
    }
    code := r.Code
    if code == 0 {
        code = http.StatusOK
    }
    c.JSON(code, r.Data)
}

// 现有的使用方式继续工作
func handler(ctx *Context) Result {
    return JSON(map[string]string{"message": "hello"})
}

server.GET("/hello", Wrap(handler))
```

#### 普通Handler不受影响

```go
// 普通Handler不受影响
func handler(ctx *Context) {
    ctx.JSON(http.StatusOK, map[string]string{"message": "hello"})
}

server.GET("/hello", handler)
```

## 3. 性能分析

### 3.1 热路径分析

```
当前流程：
Wrap(handler) -> HandlerFunc
Next() -> handler(c) -> result.Execute(c)

新流程：
Wrap(handler) -> resultHandlerWrapper
Next() -> type assert -> result.Execute(c, isLast)
```

**性能影响**：
- 每个ResultHandler多一次type assert（`ResultWithIsLast`检查）
- `isLast`计算是一次简单比较（`c.handlerIndex >= len(c.handlers)-1`）
- 结论：性能影响可忽略不计（~1-2ns）

### 3.2 优化策略

#### 优化1：内联isLast计算

```go
func (c *Context) Next() {
    c.handlerIndex++
    n := len(c.handlers)
    idx := c.handlerIndex

    for idx < n {
        if c.handlers[idx] != nil {
            handler := c.handlers[idx]

            if wrapper, ok := handler.(*resultHandlerWrapper); ok {
                result := wrapper.handler(c)
                if result != nil {
                    isLast := idx >= n-1  // 内联计算

                    if resultWithIsLast, ok := result.(ResultWithIsLast); ok {
                        resultWithIsLast.Execute(c, isLast)
                    } else if isLast {
                        result.Execute(c)
                    }
                }
            } else {
                handler(c)
            }
        }
        idx++
    }
    c.handlerIndex = idx
}
```

#### 优化2：避免重复type assert

```go
// 在Wrap时记录result类型，避免运行时type assert
type resultHandlerWrapper struct {
    handler    ResultHandler
    hasIsLast  bool  // handler返回的Result是否实现ResultWithIsLast
}

func Wrap(handler ResultHandler) HandlerFunc {
    // 预先检测handler返回的Result类型
    ctx := &Context{}  // 临时context
    result := handler(ctx)
    if result == nil {
        return &resultHandlerWrapper{handler: handler, hasIsLast: false}
    }
    _, hasIsLast := result.(ResultWithIsLast)
    return &resultHandlerWrapper{handler: handler, hasIsLast: hasIsLast}
}
```

**问题**：这需要调用handler，可能导致副作用。

**决策**：不采用优化2，保持简单。

## 4. 测试计划

### 4.1 单元测试

```go
func TestAutoResult_LastInChain(t *testing.T) {
    tests := []struct {
        name          string
        handlers      []HandlerFunc
        expectWritten bool
    }{
        {
            name: "Result is last in chain",
            handlers: []HandlerFunc{
                func(ctx *Context) { ctx.Next() },
                Wrap(func(ctx *Context) Result {
                    return Auto(map[string]string{"message": "hello"})
                }),
            },
            expectWritten: true,
        },
        {
            name: "Result is NOT last in chain",
            handlers: []HandlerFunc{
                func(ctx *Context) { ctx.Next() },
                Wrap(func(ctx *Context) Result {
                    return Auto(map[string]string{"message": "hello"})
                }),
                func(ctx *Context) {
                    ctx.JSON(http.StatusOK, map[string]string{"message": "overwritten"})
                },
            },
            expectWritten: false,  // Result的响应被覆盖
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 实现测试逻辑
        })
    }
}

func TestAutoResult_AutoMarshal(t *testing.T) {
    tests := []struct {
        name         string
        acceptHeader string
        expectedType string
    }{
        {"JSON", "application/json", "application/json"},
        {"XML", "application/xml", "application/xml"},
        {"Default JSON", "", "application/json"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 实现测试逻辑
        })
    }
}
```

### 4.2 集成测试

```go
func TestSpringBootStyle_Integration(t *testing.T) {
    server := NewServer()
    server.GET("/users/:id", Wrap(func(ctx *Context) Result {
        return Auto(UserResponse{
            ID:    1,
            Name:  "John",
            Email: "john@example.com",
        })
    }))

    // 测试GET /users/1
    // 验证：状态码200，Content-Type=application/json，body正确
}

func TestResultWithMiddleware(t *testing.T) {
    server := NewServer()
    server.Use(RequestLogger())
    server.GET("/users/:id", Wrap(func(ctx *Context) Result {
        return Auto(UserResponse{ID: 1, Name: "John"})
    }))

    // 测试：中间件 + ResultHandler正常工作
}
```

### 4.3 基准测试

```go
func BenchmarkAutoResult(b *testing.B) {
    server := NewServer()
    server.GET("/users/:id", Wrap(func(ctx *Context) Result {
        return Auto(UserResponse{ID: 1, Name: "John"})
    }))

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        req := fasthttp.AcquireRequest()
        req.Header.SetMethod("GET")
        req.SetRequestURI("/users/1")
        var ctx fasthttp.RequestCtx
        ctx.Init(req, benchAddr, nil)
        server.FastHandler(&ctx)
        fasthttp.ReleaseRequest(req)
    }
}

func BenchmarkAutoResult_vs_JsonResult(b *testing.B) {
    // 对比AutoResult和JsonResult的性能
}
```

## 5. 文档和示例

### 5.1 API文档

```go
// Auto 返回Spring Boot风格的自动marshal Result
// Result会自动根据Accept header选择JSON/XML编码
// 只有当Result是handler链的最后一个时才会写入响应
func Auto(data interface{}) Result

// AutoCode 返回带状态码的自动marshal Result
func AutoCode(data interface{}, code int) Result

// AutoWithHeaders 返回带自定义headers的自动marshal Result
func AutoWithHeaders(data interface{}, code int, headers map[string]string) Result
```

### 5.2 迁移指南

#### 从Result到AutoResult

```go
// 旧代码
func handler(ctx *Context) Result {
    return JSON(map[string]string{"message": "hello"})
}

// 新代码
func handler(ctx *Context) Result {
    return Auto(map[string]string{"message": "hello"})
}
```

#### 从普通Handler到ResultHandler

```go
// 旧代码
func handler(ctx *Context) {
    user := getUserFromDB(ctx.Param("id"))
    ctx.JSON(http.StatusOK, user)
}

// 新代码
func handler(ctx *Context) Result {
    user := getUserFromDB(ctx.Param("id"))
    return Auto(user)
}
```

## 6. 未来扩展

### 6.1 自定义编码器

```go
// 支持自定义编码器
type CustomResult struct {
    data   interface{}
    encoder func(data interface{}) ([]byte, error)
}

func (r *CustomResult) Execute(ctx *Context, isLast bool) {
    if !isLast {
        return
    }

    data, err := r.encoder(r.data)
    if err != nil {
        panic(err)
    }
    ctx.Data(http.StatusOK, "application/custom", data)
}

// 使用
func handler(ctx *Context) Result {
    return &CustomResult{
        data:   getUserFromDB(ctx.Param("id")),
        encoder: func(data interface{}) ([]byte, error) {
            // 自定义编码逻辑
        },
    }
}
```

### 6.2 流式响应

```go
// 支持流式响应
type StreamResult struct {
    reader io.Reader
}

func (r *StreamResult) Execute(ctx *Context, isLast bool) {
    if !isLast {
        return
    }

    ctx.Header("Content-Type", "application/octet-stream")
    ctx.Status(http.StatusOK)
    _, _ = io.Copy(ctx.Writer, r.reader)
}

// 使用
func handler(ctx *Context) Result {
    return &StreamResult{
        reader: getStreamFromDB(),
    }
}
```

## 7. 总结

### 7.1 设计优势
1. **Spring Boot风格**：支持handler直接返回数据，减少模板代码
2. **向后兼容**：现有Result和Handler继续工作
3. **零性能开销**：热路径只有一次type assert
4. **灵活性强**：支持自定义Result实现

### 7.2 实施步骤
1. 添加`ResultWithIsLast`接口
2. 实现`AutoResult`及相关便捷函数
3. 修改`Context.Next()`支持ResultWithIsLast
4. 编写单元测试
5. 编写集成测试
6. 编写基准测试
7. 更新文档

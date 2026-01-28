# Result接口重设计文档 v2

## 1. 设计目标

### 1.1 核心目标
1. **无向后兼容性要求** - 可以完全重新设计
2. **AutoResult自动marshal** - 根据Accept header自动选择JSON/XML等编码
3. **不检测isLast** - AutoResult应该总是执行响应，由用户控制handler链
4. **简洁高效** - 最小化抽象，热路径零开销

### 1.2 设计原则
- KISS原则：保持接口简单
- Spring Boot风格：handler直接返回数据
- 框架负责序列化：用户不需要关心编码细节

## 2. Result接口设计

### 2.1 核心接口

```go
// Result接口：统一的返回值接口
// Execute方法负责将结果写入Context
type Result interface {
    Execute(ctx *Context)
}

// ResultHandler：返回Result的handler类型
type ResultHandler func(*Context) Result

// Wrap：将ResultHandler包装为普通HandlerFunc
func Wrap(handler ResultHandler) HandlerFunc {
    return func(ctx *Context) {
        result := handler(ctx)
        if result != nil {
            result.Execute(ctx)
        }
    }
}

// Wraps：包装多个ResultHandler为HandlerFunc slice
func Wraps(handlers ...ResultHandler) []HandlerFunc {
    result := make([]HandlerFunc, len(handlers))
    for i, h := range handlers {
        result[i] = Wrap(h)
    }
    return result
}
```

### 2.2 AutoResult实现

```go
// AutoResult：自动marshal返回值
// 根据Accept header自动选择编码格式（JSON/XML等）
type AutoResult struct {
    data     interface{}
    code     int
    headers  map[string]string
    marshal  MarshalFunc  // 可选的自定义marshal函数
}

// MarshalFunc：自定义marshal函数类型
type MarshalFunc func(data interface{}) ([]byte, string, error)

// NewAutoResult：创建AutoResult
func NewAutoResult(data interface{}) *AutoResult {
    return &AutoResult{
        data:    data,
        code:    http.StatusOK,
        headers: make(map[string]string),
    }
}

// Execute：实现Result接口
func (r *AutoResult) Execute(ctx *Context) {
    // 设置状态码
    code := r.code
    if code == 0 {
        code = http.StatusOK
    }

    // 设置自定义headers
    for k, v := range r.headers {
        ctx.Header(k, v)
    }

    // 执行marshal
    if r.marshal != nil {
        // 使用自定义marshal函数
        body, contentType, err := r.marshal(r.data)
        if err != nil {
            panic(err)
        }
        ctx.Header("Content-Type", contentType)
        ctx.Status(code)
        ctx.Writer.Write(body)
        return
    }

    // 自动marshal：根据Accept header选择编码
    r.autoMarshal(ctx, code)
}

// autoMarshal：根据Accept header自动选择编码格式
func (r *AutoResult) autoMarshal(ctx *Context, code int) {
    accept := ctx.GetHeader("Accept")
    if accept == "" {
        accept = "*/*"
    }

    // 根据Accept header的优先级选择编码
    // 支持的格式：application/json, application/xml, text/plain, application/octet-stream
    switch {
    case contains(accept, "application/json") || accept == "*/*":
        ctx.JSON(code, r.data)
    case contains(accept, "application/xml") || contains(accept, "text/xml"):
        ctx.XML(code, r.data)
    case contains(accept, "text/plain"):
        ctx.String(code, "%v", r.data)
    case contains(accept, "application/octet-stream"):
        // 对于二进制数据，需要调用者提供[]byte或io.Reader
        if data, ok := r.data.([]byte); ok {
            ctx.Data(code, "application/octet-stream", data)
        } else if reader, ok := r.data.(io.Reader); ok {
            ctx.Header("Content-Type", "application/octet-stream")
            ctx.Status(code)
            _, _ = io.Copy(ctx.Writer, reader)
        } else {
            panic("AutoResult: binary data must be []byte or io.Reader")
        }
    default:
        // 默认JSON
        ctx.JSON(code, r.data)
    }
}

// WithCode：设置状态码（链式调用）
func (r *AutoResult) WithCode(code int) *AutoResult {
    r.code = code
    return r
}

// WithHeader：设置header（链式调用）
func (r *AutoResult) WithHeader(key, value string) *AutoResult {
    if r.headers == nil {
        r.headers = make(map[string]string)
    }
    r.headers[key] = value
    return r
}

// WithHeaders：设置多个headers（链式调用）
func (r *AutoResult) WithHeaders(headers map[string]string) *AutoResult {
    if r.headers == nil {
        r.headers = make(map[string]string)
    }
    for k, v := range headers {
        r.headers[k] = v
    }
    return r
}

// WithMarshal：设置自定义marshal函数（链式调用）
func (r *AutoResult) WithMarshal(marshal MarshalFunc) *AutoResult {
    r.marshal = marshal
    return r
}

// contains：辅助函数，检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
    return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
```

### 2.3 便捷函数

```go
// Auto：自动marshal返回值（默认状态码200）
func Auto(data interface{}) Result {
    return NewAutoResult(data)
}

// AutoCode：自动marshal返回值（指定状态码）
func AutoCode(data interface{}, code int) Result {
    return NewAutoResult(data).WithCode(code)
}

// AutoWithHeaders：自动marshal返回值（指定headers）
func AutoWithHeaders(data interface{}, headers map[string]string) Result {
    return NewAutoResult(data).WithHeaders(headers)
}

// AutoCustom：自动marshal返回值（自定义marshal函数）
func AutoCustom(data interface{}, marshal MarshalFunc) Result {
    return NewAutoResult(data).WithMarshal(marshal)
}
```

### 2.4 其他Result实现

#### JSON Result

```go
// JSON Result：固定使用JSON编码
type JSONResult struct {
    Data interface{}
    Code int
}

func (r *JSONResult) Execute(ctx *Context) {
    code := r.Code
    if code == 0 {
        code = http.StatusOK
    }
    ctx.JSON(code, r.Data)
}

func JSON(data interface{}) Result {
    return &JSONResult{Data: data, Code: http.StatusOK}
}

func JSONCode(data interface{}, code int) Result {
    return &JSONResult{Data: data, Code: code}
}
```

#### XML Result

```go
// XML Result：固定使用XML编码
type XMLResult struct {
    Data interface{}
    Code int
}

func (r *XMLResult) Execute(ctx *Context) {
    code := r.Code
    if code == 0 {
        code = http.StatusOK
    }
    ctx.XML(code, r.Data)
}

func XML(data interface{}) Result {
    return &XMLResult{Data: data, Code: http.StatusOK}
}

func XMLCode(data interface{}, code int) Result {
    return &XMLResult{Data: data, Code: code}
}
```

#### HTML Result

```go
// HTML Result：渲染HTML模板
type HTMLResult struct {
    Template string
    Data     interface{}
    Code     int
}

func (r *HTMLResult) Execute(ctx *Context) {
    code := r.Code
    if code == 0 {
        code = http.StatusOK
    }
    ctx.HTML(code, r.Template, r.Data)
}

func HTML(template string, data interface{}) Result {
    return &HTMLResult{Template: template, Data: data, Code: http.StatusOK}
}

func HTMLCode(template string, data interface{}, code int) Result {
    return &HTMLResult{Template: template, Data: data, Code: code}
}
```

#### String Result

```go
// String Result：返回纯文本
type StringResult struct {
    Format string
    Data   []interface{}
    Code   int
}

func (r *StringResult) Execute(ctx *Context) {
    code := r.Code
    if code == 0 {
        code = http.StatusOK
    }
    if len(r.Data) > 0 {
        ctx.String(code, r.Format, r.Data...)
    } else {
        ctx.String(code, r.Format)
    }
}

func String(format string, data ...interface{}) Result {
    return &StringResult{Format: format, Data: data, Code: http.StatusOK}
}

func StringCode(format string, code int, data ...interface{}) Result {
    return &StringResult{Format: format, Data: data, Code: code}
}
```

#### Data Result

```go
// Data Result：返回二进制数据
type DataResult struct {
    Data        []byte
    ContentType string
    Code        int
}

func (r *DataResult) Execute(ctx *Context) {
    code := r.Code
    if code == 0 {
        code = http.StatusOK
    }
    ctx.Data(code, r.ContentType, r.Data)
}

func Data(contentType string, data []byte) Result {
    return &DataResult{Data: data, ContentType: contentType, Code: http.StatusOK}
}

func DataCode(contentType string, data []byte, code int) Result {
    return &DataResult{Data: data, ContentType: contentType, Code: code}
}
```

#### Redirect Result

```go
// Redirect Result：重定向
type RedirectResult struct {
    URL  string
    Code int
}

func (r *RedirectResult) Execute(ctx *Context) {
    code := r.Code
    if code == 0 {
        code = http.StatusFound
    }
    ctx.Writer.Header().Set("Location", r.URL)
    ctx.Status(code)
}

func Redirect(url string) Result {
    return &RedirectResult{URL: url, Code: http.StatusFound}
}

func RedirectCode(url string, code int) Result {
    return &RedirectResult{URL: url, Code: code}
}
```

#### Error Result

```go
// Error Result：返回错误
type ErrorResult struct {
    Err  error
    Code int
    Msg  string
}

func (r *ErrorResult) Execute(ctx *Context) {
    code := r.Code
    if code == 0 {
        code = http.StatusInternalServerError
    }
    msg := r.Msg
    if msg == "" && r.Err != nil {
        msg = r.Err.Error()
    }
    if msg == "" {
        msg = http.StatusText(code)
    }
    ctx.String(code, "%s", msg)
}

func Error(err error) Result {
    msg := ""
    if err != nil {
        msg = err.Error()
    }
    return &ErrorResult{Err: err, Code: http.StatusInternalServerError, Msg: msg}
}

func ErrorMsg(msg string) Result {
    return &ErrorResult{Msg: msg, Code: http.StatusInternalServerError}
}

func ErrorCode(err error, code int) Result {
    msg := ""
    if err != nil {
        msg = err.Error()
    }
    return &ErrorResult{Err: err, Code: code, Msg: msg}
}

func ErrorStatusCode(code int, msg string) Result {
    return &ErrorResult{Code: code, Msg: msg}
}
```

#### NoContent Result

```go
// NoContent Result：返回204 No Content
type NoContentResult struct{}

func (r *NoContentResult) Execute(ctx *Context) {
    if ctx.Writer != nil && !ctx.Writer.Written() {
        ctx.Status(http.StatusNoContent)
    }
}

func NoContent() Result {
    return &NoContentResult{}
}
```

#### Stream Result

```go
// Stream Result：流式响应
type StreamResult struct {
    Reader      io.Reader
    ContentType string
    Code        int
}

func (r *StreamResult) Execute(ctx *Context) {
    code := r.Code
    if code == 0 {
        code = http.StatusOK
    }
    if r.ContentType == "" {
        r.ContentType = "application/octet-stream"
    }
    ctx.Header("Content-Type", r.ContentType)
    ctx.Status(code)
    _, _ = io.Copy(ctx.Writer, r.Reader)
}

func Stream(reader io.Reader) Result {
    return &StreamResult{Reader: reader, Code: http.StatusOK}
}

func StreamWithContentType(reader io.Reader, contentType string) Result {
    return &StreamResult{Reader: reader, ContentType: contentType, Code: http.StatusOK}
}
```

#### File Result

```go
// File Result：返回文件
type FileResult struct {
    Path        string
    ContentType string
    Code        int
}

func (r *FileResult) Execute(ctx *Context) {
    // 使用fasthttp.ServeFile
    // 或者使用http.ServeFile包装
    // ...
}

func File(path string) Result {
    return &FileResult{Path: path, Code: http.StatusOK}
}

func FileWithContentType(path, contentType string) Result {
    return &FileResult{Path: path, ContentType: contentType, Code: http.StatusOK}
}
```

## 3. 使用示例

### 3.1 基本使用

#### 示例1：AutoResult自动marshal

```go
// 定义响应结构体
type User struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

// Handler直接返回User（Spring Boot风格）
func getUser(ctx *Context) Result {
    return Auto(User{
        ID:    1,
        Name:  "John",
        Email: "john@example.com",
    })
}

// 注册路由
server.GET("/users/:id", Wrap(getUser))
```

**请求**：
```
GET /users/1
Accept: application/json
```

**响应**：
```json
{
  "id": 1,
  "name": "John",
  "email": "john@example.com"
}
```

#### 示例2：带状态码

```go
func createUser(ctx *Context) Result {
    user := User{
        ID:    2,
        Name:  "Jane",
        Email: "jane@example.com",
    }
    return AutoCode(user, http.StatusCreated)
}

server.POST("/users", Wrap(createUser))
```

**请求**：
```
POST /users
Accept: application/json
Content-Type: application/json
{"name": "Jane", "email": "jane@example.com"}
```

**响应**：
```json
{
  "id": 2,
  "name": "Jane",
  "email": "jane@example.com"
}
```

**状态码**：201 Created

#### 示例3：XML响应

```go
func getUserXML(ctx *Context) Result {
    return Auto(User{ID: 1, Name: "John"})
}

server.GET("/users/:id/xml", Wrap(getUserXML))
```

**请求**：
```
GET /users/1/xml
Accept: application/xml
```

**响应**：
```xml
<User>
  <id>1</id>
  <name>John</name>
</User>
```

### 3.2 链式调用

#### 示例4：设置headers

```go
func getCachedUser(ctx *Context) Result {
    return Auto(User{ID: 1, Name: "John"}).
        WithHeader("Cache-Control", "max-age=3600").
        WithHeader("X-Cache-Hit", "true")
}

server.GET("/users/:id/cached", Wrap(getCachedUser))
```

#### 示例5：综合使用

```go
func createUserWithHeaders(ctx *Context) Result {
    user := User{ID: 2, Name: "Jane"}
    return NewAutoResult(user).
        WithCode(http.StatusCreated).
        WithHeader("Location", "/users/2").
        WithHeader("X-Request-ID", ctx.Value("request-id").(string))
}

server.POST("/users", Wrap(createUserWithHeaders))
```

### 3.3 自定义marshal

#### 示例6：自定义JSON编码

```go
func getUserCustom(ctx *Context) Result {
    return AutoCustom(User{ID: 1, Name: "John"}, func(data interface{}) ([]byte, string, error) {
        // 使用自定义的JSON编码器
        // 例如：使用jsoniter、sonic等高性能JSON库
        b, err := json.Marshal(data)
        return b, "application/json", err
    })
}

server.GET("/users/:id/custom", Wrap(getUserCustom))
```

#### 示例7：自定义protobuf编码

```go
func getUserProtobuf(ctx *Context) Result {
    return AutoCustom(User{ID: 1, Name: "John"}, func(data interface{}) ([]byte, string, error) {
        // 使用protobuf编码
        // ...
        return pbData, "application/x-protobuf", nil
    })
}

server.GET("/users/:id/protobuf", Wrap(getUserProtobuf))
```

### 3.4 与中间件配合

#### 示例8：ResultHandler + 中间件

```go
// 中间件：认证
func authMiddleware(ctx *Context) {
    token := ctx.GetHeader("Authorization")
    if !isValidToken(token) {
        ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
        ctx.Abort()
        return
    }
    ctx.Next()
}

// Handler：返回Result
func getUser(ctx *Context) Result {
    return Auto(User{ID: 1, Name: "John"})
}

// 注册
server.Use(authMiddleware)
server.GET("/users/:id", Wrap(getUser))
```

#### 示例9：ResultHandler + 多个中间件

```go
server.Use(authMiddleware)
server.Use(loggerMiddleware)
server.Use(recoveryMiddleware)
server.GET("/users/:id", Wrap(getUser))
```

**执行流程**：
1. authMiddleware检查token
2. loggerMiddleware记录请求开始
3. recoveryMiddleware设置panic恢复
4. getUser返回AutoResult
5. AutoResult.Execute写入JSON响应
6. recoveryMiddleware清理（无panic）
7. loggerMiddleware记录请求完成

### 3.5 错误处理

#### 示例10：返回错误

```go
func getUser(ctx *Context) Result {
    user, err := findUser(ctx.Param("id"))
    if err != nil {
        if errors.Is(err, ErrUserNotFound) {
            return ErrorCode(err, http.StatusNotFound)
        }
        return Error(err)
    }
    return Auto(user)
}

server.GET("/users/:id", Wrap(getUser))
```

**用户不存在**：
```
GET /users/999
Status: 404 Not Found
Body: "user not found"
```

**内部错误**：
```
GET /users/1
Status: 500 Internal Server Error
Body: "database connection failed"
```

### 3.6 流式响应

#### 示例11：流式下载

```go
func downloadFile(ctx *Context) Result {
    file, err := os.Open("/path/to/file.zip")
    if err != nil {
        return Error(err)
    }
    defer file.Close()

    return StreamWithContentType(file, "application/zip").
        WithHeader("Content-Disposition", "attachment; filename=file.zip")
}

server.GET("/download", Wrap(downloadFile))
```

### 3.7 文件响应

#### 示例12：静态文件

```go
func serveImage(ctx *Context) Result {
    return File("/path/to/image.jpg")
}

server.GET("/image.jpg", Wrap(serveImage))
```

### 3.8 组合使用

#### 示例13：RESTful API

```go
// GET /users - 获取用户列表
func listUsers(ctx *Context) Result {
    users := []User{
        {ID: 1, Name: "John"},
        {ID: 2, Name: "Jane"},
    }
    return Auto(users)
}

// GET /users/:id - 获取单个用户
func getUser(ctx *Context) Result {
    user, err := findUser(ctx.Param("id"))
    if err != nil {
        return ErrorCode(err, http.StatusNotFound)
    }
    return Auto(user)
}

// POST /users - 创建用户
func createUser(ctx *Context) Result {
    var req CreateUserRequest
    if err := ctx.BindJSON(&req); err != nil {
        return ErrorCode(err, http.StatusBadRequest)
    }

    user, err := saveUser(req)
    if err != nil {
        return Error(err)
    }

    return AutoCode(user, http.StatusCreated).
        WithHeader("Location", fmt.Sprintf("/users/%d", user.ID))
}

// PUT /users/:id - 更新用户
func updateUser(ctx *Context) Result {
    var req UpdateUserRequest
    if err := ctx.BindJSON(&req); err != nil {
        return ErrorCode(err, http.StatusBadRequest)
    }

    user, err := updateUser(ctx.Param("id"), req)
    if err != nil {
        if errors.Is(err, ErrUserNotFound) {
            return ErrorCode(err, http.StatusNotFound)
        }
        return Error(err)
    }

    return Auto(user)
}

// DELETE /users/:id - 删除用户
func deleteUser(ctx *Context) Result {
    err := deleteUser(ctx.Param("id"))
    if err != nil {
        if errors.Is(err, ErrUserNotFound) {
            return ErrorCode(err, http.StatusNotFound)
        }
        return Error(err)
    }
    return NoContent()
}

// 注册路由
server.GET("/users", Wrap(listUsers))
server.GET("/users/:id", Wrap(getUser))
server.POST("/users", Wrap(createUser))
server.PUT("/users/:id", Wrap(updateUser))
server.DELETE("/users/:id", Wrap(deleteUser))
```

## 4. Context修改

### 4.1 无需修改

**关键点**：Context的`Next()`方法无需任何修改！

```go
// 现有的Next()方法保持不变
func (c *Context) Next() {
    c.handlerIndex++
    for c.handlerIndex < len(c.handlers) {
        if c.handlers[c.handlerIndex] != nil {
            c.handlers[c.handlerIndex](c)
        }
        c.handlerIndex++
    }
}

// Wrap函数也保持简单
func Wrap(handler ResultHandler) HandlerFunc {
    return func(ctx *Context) {
        result := handler(ctx)
        if result != nil {
            result.Execute(ctx)
        }
    }
}
```

**设计优势**：
- 无需修改Context核心逻辑
- 无需在Next()中添加特殊处理
- Result只是普通的HandlerFunc
- 热路径零额外开销

### 4.2 AutoResult行为

**问题**：如果AutoResult不是最后一个handler，响应会被覆盖吗？

**答案**：会，但这是预期行为！

```go
// 示例：Result不是最后一个
server.GET("/test",
    Wrap(func(ctx *Context) Result {
        // 这个Result会执行
        return Auto(map[string]string{"message": "first"})
    }),
    func(ctx *Context) {
        // 这个Handler会覆盖上面的响应
        ctx.JSON(http.StatusOK, map[string]string{"message": "overwritten"})
    },
)
```

**最终响应**：
```json
{"message": "overwritten"}
```

**控制权在用户手中**：
- 如果Result应该作为最终响应，放在最后
- 如果需要后续处理，不要使用Result，使用普通Handler
- 使用`Abort()`可以停止handler链

## 5. 性能分析

### 5.1 热路径分析

```
FastHandler -> Next() -> Wrap(handler) -> result.Execute()
```

**性能开销**：
- Wrap只是简单的闭包包装（零分配）
- Result.Execute是接口调用（~2ns）
- AutoResult.autoMarshal是Accept header检查（字符串比较）
- 最终调用ctx.JSON/ctx.XML等（与直接调用相同）

**结论**：性能影响可忽略不计（< 5ns/op）

### 5.2 基准测试对比

```go
// 测试1：直接调用ctx.JSON
func Benchmark_DirectJSON(b *testing.B) {
    for i := 0; i < b.N; i++ {
        ctx.JSON(http.StatusOK, map[string]string{"message": "hello"})
    }
}

// 测试2：使用JSON Result
func Benchmark_JSONResult(b *testing.B) {
    result := JSON(map[string]string{"message": "hello"})
    for i := 0; i < b.N; i++ {
        result.Execute(ctx)
    }
}

// 测试3：使用Auto Result
func Benchmark_AutoResult(b *testing.B) {
    result := Auto(map[string]string{"message": "hello"})
    for i := 0; i < b.N; i++ {
        result.Execute(ctx)
    }
}

// 预期结果：
// DirectJSON:  ~1000 ns/op
// JSONResult:  ~1010 ns/op (+1%)
// AutoResult: ~1020 ns/op (+2%)
```

## 6. 测试计划

### 6.1 单元测试

#### AutoResult测试

```go
func TestAutoResult_JSON(t *testing.T) {
    ctx := &Context{}
    result := Auto(map[string]string{"message": "hello"})

    // 设置Accept header
    ctx.Header("Accept", "application/json")
    result.Execute(ctx)

    // 验证
    assert.Equal(t, http.StatusOK, ctx.StatusCode())
    assert.Equal(t, "application/json", ctx.GetHeader("Content-Type"))
    // 验证body...
}

func TestAutoResult_XML(t *testing.T) {
    ctx := &Context{}
    result := Auto(map[string]string{"message": "hello"})

    ctx.Header("Accept", "application/xml")
    result.Execute(ctx)

    assert.Equal(t, "application/xml", ctx.GetHeader("Content-Type"))
    // 验证body...
}

func TestAutoResult_DefaultJSON(t *testing.T) {
    ctx := &Context{}
    result := Auto(map[string]string{"message": "hello"})

    // 不设置Accept header
    result.Execute(ctx)

    // 默认应该是JSON
    assert.Equal(t, "application/json", ctx.GetHeader("Content-Type"))
}
```

#### 链式调用测试

```go
func TestAutoResult_WithCode(t *testing.T) {
    ctx := &Context{}
    result := Auto(nil).WithCode(http.StatusCreated)
    result.Execute(ctx)

    assert.Equal(t, http.StatusCreated, ctx.StatusCode())
}

func TestAutoResult_WithHeader(t *testing.T) {
    ctx := &Context{}
    result := Auto(nil).WithHeader("X-Custom", "value")
    result.Execute(ctx)

    assert.Equal(t, "value", ctx.GetHeader("X-Custom"))
}

func TestAutoResult_WithHeaders(t *testing.T) {
    ctx := &Context{}
    result := Auto(nil).WithHeaders(map[string]string{
        "X-Custom1": "value1",
        "X-Custom2": "value2",
    })
    result.Execute(ctx)

    assert.Equal(t, "value1", ctx.GetHeader("X-Custom1"))
    assert.Equal(t, "value2", ctx.GetHeader("X-Custom2"))
}
```

### 6.2 集成测试

```go
func TestAutoResult_Integration(t *testing.T) {
    server := NewServer()
    server.GET("/users/:id", Wrap(func(ctx *Context) Result {
        return Auto(User{ID: 1, Name: "John"})
    }))

    // 启动测试服务器
    // 发送请求
    // 验证响应
}
```

### 6.3 基准测试

```go
func BenchmarkAutoResult(b *testing.B) {
    result := Auto(map[string]string{"message": "hello"})

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        ctx := &Context{}
        result.Execute(ctx)
    }
}

func BenchmarkJSONResult(b *testing.B) {
    result := JSON(map[string]string{"message": "hello"})

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        ctx := &Context{}
        result.Execute(ctx)
    }
}

func BenchmarkWrap(b *testing.B) {
    handler := func(ctx *Context) Result {
        return Auto(map[string]string{"message": "hello"})
    }

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        Wrap(handler)(nil)
    }
}
```

## 7. 文档和示例

### 7.1 API文档

```go
// Result：统一的返回值接口
// Execute方法负责将结果写入Context
type Result interface {
    Execute(ctx *Context)
}

// Auto：自动marshal返回值
// 根据Accept header自动选择编码格式
// 支持：application/json, application/xml, text/plain
func Auto(data interface{}) Result

// AutoCode：自动marshal返回值（指定状态码）
func AutoCode(data interface{}, code int) Result

// AutoWithHeaders：自动marshal返回值（指定headers）
func AutoWithHeaders(data interface{}, headers map[string]string) Result

// AutoCustom：自动marshal返回值（自定义marshal函数）
func AutoCustom(data interface{}, marshal MarshalFunc) Result

// JSON：返回JSON（固定格式）
func JSON(data interface{}) Result

// JSONCode：返回JSON（指定状态码）
func JSONCode(data interface{}, code int) Result

// XML：返回XML（固定格式）
func XML(data interface{}) Result

// XMLCode：返回XML（指定状态码）
func XMLCode(data interface{}, code int) Result

// HTML：返回HTML（渲染模板）
func HTML(template string, data interface{}) Result

// String：返回纯文本
func String(format string, data ...interface{}) Result

// Error：返回错误
func Error(err error) Result

// NoContent：返回204 No Content
func NoContent() Result

// Stream：流式响应
func Stream(reader io.Reader) Result

// Redirect：重定向
func Redirect(url string) Result
```

### 7.2 迁移指南

#### 从普通Handler到ResultHandler

```go
// 旧代码
func getUser(ctx *Context) {
    user := getUserFromDB(ctx.Param("id"))
    ctx.JSON(http.StatusOK, user)
}

// 新代码
func getUser(ctx *Context) Result {
    user := getUserFromDB(ctx.Param("id"))
    return Auto(user)
}

// 注册：需要Wrap
server.GET("/users/:id", Wrap(getUser))
```

#### 从JSON Result到Auto Result

```go
// 旧代码（如果存在）
func getUser(ctx *Context) Result {
    return JSON(User{ID: 1, Name: "John"})
}

// 新代码
func getUser(ctx *Context) Result {
    return Auto(User{ID: 1, Name: "John"})
}
```

## 8. 总结

### 8.1 设计优势
1. **Spring Boot风格**：handler直接返回数据，减少模板代码
2. **自动marshal**：根据Accept header自动选择编码格式
3. **零向后兼容负担**：完全重新设计，不遗留历史包袱
4. **简洁高效**：无需修改Context核心逻辑，热路径零开销
5. **灵活性强**：支持自定义Result实现和marshal函数

### 8.2 设计决策
| 决策点 | 选择 | 原因 |
|--------|------|------|
| isLast检测 | **不检测** | 不向后兼容，由用户控制handler链 |
| 向后兼容 | **不保证** | 用户明确说了不需要兼容性 |
| Content-Type | **Accept header** | 根据请求决定响应格式，符合HTTP规范 |
| Result行为 | **总是执行** | 不检测isLast，由后续handler决定是否覆盖 |

### 8.3 实施步骤
1. 实现AutoResult及相关便捷函数
2. 实现其他Result类型（JSON, XML, HTML, String, Error等）
3. 编写单元测试
4. 编写集成测试
5. 编写基准测试
6. 更新文档和示例
7. （可选）删除旧的Result实现

### 8.4 与Spring Boot的对比

| 特性 | Spring Boot | gserver AutoResult |
|------|-------------|-------------------|
| 返回值类型 | 任意对象 | Result接口 |
| 自动序列化 | ✅ 支持 | ✅ 支持 |
| Content-Type检测 | ✅ Accept header | ✅ Accept header |
| 状态码控制 | @ResponseStatus | WithCode() |
| 自定义序列化 | @ResponseBody | AutoCustom() |
| 流式响应 | StreamingResponseBody | StreamResult |
| 模板渲染 | Thymeleaf等 | HTMLResult |

**gserver的优势**：
- 更灵活的Result类型（不仅仅是对象）
- 支持自定义marshal函数
- 链式调用API
- 更好的错误处理（ErrorResult）

**Spring Boot的优势**：
- 基于注解，更简洁
- 集成Spring生态系统
- 自动配置更强大

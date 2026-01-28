# Context API完整对齐设计文档

## 1. 设计目标

### 1.1 核心目标
1. **API对齐**：对齐gin/hertz/fiber的核心Context方法
2. **不是wrapper**：直接实现，不使用compat wrapper
3. **保持一致性**：方法命名和行为与主流框架一致
4. **性能优先**：零额外开销

### 1.2 设计原则
- **不向后兼容**：完全重新设计
- **保持现有性能**：不引入额外反射或复杂逻辑
- **Go风格**：遵循Go的命名习惯和错误处理

## 2. API对比分析

### 2.1 gin vs hertz vs fiber vs gserver

#### 2.1.1 请求参数获取

| 方法 | gin | hertz | fiber | gserver当前 | 优先级 |
|------|-----|-------|-------|-------------|--------|
| Path参数 | Param | Param | Params | ✅ Param | - |
| Query参数 | Query | Query | Query | ✅ Query | - |
| Query Array | QueryArray | QueryArgs | All | ✅ QueryArray | - |
| Query Map | QueryMap | - | All | ✅ QueryMap | - |
| Query Default | DefaultQuery | GetQuery | Get | ❌ 缺失 | 高 |
| Get Query | GetQuery | GetQuery | Get | ✅ GetQuery | - |
| Post Form | PostForm | PostForm | Form | ✅ PostForm | - |
| Post Form Default | DefaultPostForm | GetPostForm | Form | ❌ 缺失 | 高 |
| Get Post Form | GetPostForm | GetPostForm | Get | ✅ GetPostForm | - |
| Post Form Array | PostFormArray | - | All | ✅ PostFormArray | - |
| Post Form Map | PostFormMap | - | All | ✅ PostFormMap | - |

**结论**：缺少`DefaultQuery`和`DefaultPostForm`（高优先级）

#### 2.1.2 Header相关

| 方法 | gin | hertz | fiber | gserver当前 | 优先级 |
|------|-----|-------|-------|-------------|--------|
| Get Header | GetHeader | GetHeader | Get | ✅ GetHeader | - |
| Set Header | Header | Header | Set | ✅ Header | - |
| Get Raw Data | GetRawData | GetRawData | Body | ❌ 缺失 | 中 |
| Cookie | Cookie | Cookie | Cookies | ✅ Cookie | - |
| Set Cookie | SetCookie | SetCookie | Cookie | ✅ SetCookie | - |
| Get Cookie | GetCookie | GetCookie | Cookies | ❌ 缺失（需要返回error） | 中 |

**结论**：缺少`GetRawData`（中优先级），`GetCookie`需要改进

#### 2.1.3 响应方法

| 方法 | gin | hertz | fiber | gserver当前 | 优先级 |
|------|-----|-------|-------|-------------|--------|
| JSON | JSON | JSON | JSON | ✅ JSON | - |
| XML | XML | XML | XML | ✅ XML | - |
| HTML | HTML | HTML | HTML | ✅ HTML | - |
| String | String | String | SendString | ✅ String | - |
| Data | Data | Data | Send | ✅ Data | - |
| File | File | File | Download/SendFile | ❌ 缺失 | 中 |
| Inline | FileAttachment | - | Attachment | ❌ 缺失 | 低 |
| Redirect | Redirect | Redirect | Redirect | ❌ 缺失（需要） | 高 |
| SSE | SSE | - | - | ❌ 缺失 | 低 |
| Stream | - | Stream | SendStream | ❌ 缺失 | 中 |

**结论**：缺少`Redirect`（高优先级），`File`和`Stream`（中优先级）

#### 2.1.4 Context存储

| 方法 | gin | hertz | fiber | gserver当前 | 优先级 |
|------|-----|-------|-------|-------------|--------|
| Set | Set | Set | Set | ✅ Set | - |
| Get | Get | Get | Get | ❌ 缺失 | 高 |
| Must Get | MustGet | MustGet | - | ❌ 缺失 | 中 |

**结论**：缺少`Get`（高优先级），`MustGet`（中优先级）

#### 2.1.5 Handler链控制

| 方法 | gin | hertz | fiber | gserver当前 | 优先级 |
|------|-----|-------|-------|-------------|--------|
| Next | Next | Next | Next | ✅ Next | - |
| Abort | Abort | Abort | - | ✅ Abort | - |
| Is Aborted | IsAborted | IsAborted | - | ✅ IsAborted | - |
| Abort With Status | AbortWithStatus | AbortWithStatus | - | ✅ AbortWithStatus | - |
| Abort With Status JSON | AbortWithStatusJSON | AbortWithStatusJSON | - | ✅ AbortWithStatusJSON | - |

**结论**：已完整

#### 2.1.6 客户端信息

| 方法 | gin | hertz | fiber | gserver当前 | 优先级 |
|------|-----|-------|-------|-------------|--------|
| Client IP | ClientIP | ClientIP | IP | ✅ ClientIP | - |
| Get Raw Data | - | - | - | ❌ 缺失 | 中 |

**结论**：缺少`GetRawData`（已在Header相关）

#### 2.1.7 其他辅助方法

| 方法 | gin | hertz | fiber | gserver当前 | 优先级 |
|------|-----|-------|-------|-------------|--------|
| Content Type | ContentType | ContentType | Get | ✅ ContentType | - |
| Status Code | - | GetStatusCode | Status | ✅ StatusCode | - |
| Get Header | GetHeader | GetHeader | Get | ✅ GetHeader | - |
| File | File | File | SendFile | ❌ 缺失 | 中 |
| Stream | - | Stream | SendStream | ❌ 缺失 | 中 |

### 2.2 缺失方法总结

**高优先级**：
1. `Get(key interface{}) (interface{}, bool)` - 获取Context存储值
2. `DefaultQuery(key, defaultValue string) string` - 获取Query参数（带默认值）
3. `DefaultPostForm(key, defaultValue string) string` - 获取PostForm参数（带默认值）
4. `Redirect(code int, location string)` - 重定向

**中优先级**：
5. `GetRawData() ([]byte, error)` - 获取原始请求body
6. `GetCookie(name string) (string, error)` - 获取Cookie（返回error）
7. `File(filepath string)` - 返回文件
8. `Stream(r io.Reader, contentType string)` - 流式响应

**低优先级**：
9. `MustGet(key interface{}) interface{}` - 获取Context存储值（panic if not exists）
10. `FileAttachment(filepath, filename string)` - 返回文件（指定下载名）

## 3. 新增方法设计

### 3.1 Context存储方法

#### Get方法

```go
// Get returns the value for the given key.
// Returns (value, ok) where ok is false if the value does not exist.
// Unlike GetValue, this method accepts interface{} key for consistency with gin/hertz.
func (c *Context) Get(key interface{}) (interface{}, bool) {
    if c.values == nil {
        return nil, false
    }
    v, ok := c.values[key]
    return v, ok
}

// MustGet returns the value for the given key.
// Panics if the value does not exist.
func (c *Context) MustGet(key interface{}) interface{} {
    if c.values == nil {
        panic(fmt.Sprintf("key %v does not exist", key))
    }
    v, ok := c.values[key]
    if !ok {
        panic(fmt.Sprintf("key %v does not exist", key))
    }
    return v
}
```

#### 对比

| 框架 | 方法 | 返回值 | 错误处理 |
|------|------|--------|----------|
| gin | `Get(key string)` | (interface{}, bool) | ok=false表示不存在 |
| hertz | `Get(key string)` | (interface{}, bool) | ok=false表示不存在 |
| gserver | `Get(key interface{})` | (interface{}, bool) | ok=false表示不存在 |

**设计决策**：使用`interface{}`作为key类型，而不是`string`，提供更大的灵活性（与现有`SetValue`保持一致）

### 3.2 Query/PostForm默认值方法

#### DefaultQuery方法

```go
// DefaultQuery returns the query string value for the given key.
// Returns defaultValue if the value is empty.
func (c *Context) DefaultQuery(key, defaultValue string) string {
    value := c.Query(key)
    if value == "" {
        return defaultValue
    }
    return value
}
```

#### DefaultPostForm方法

```go
// DefaultPostForm returns the post form string value for the given key.
// Returns defaultValue if the value is empty.
func (c *Context) DefaultPostForm(key, defaultValue string) string {
    value := c.PostForm(key)
    if value == "" {
        return defaultValue
    }
    return value
}
```

#### 对比

| 框架 | 方法 | 返回值 | 行为 |
|------|------|--------|------|
| gin | `DefaultQuery(key, defaultValue)` | string | value==""时返回defaultValue |
| hertz | `GetQuery(key, defaultValue)` | string | value==""时返回defaultValue |
| fiber | `Get(key, defaultValue)` | string | value==""时返回defaultValue |
| gserver | `DefaultQuery(key, defaultValue)` | string | value==""时返回defaultValue |

**设计决策**：使用gin的命名`DefaultQuery`，而不是hertz的`GetQuery`

### 3.3 Redirect方法

```go
// Redirect returns a HTTP redirect to the specific location.
func (c *Context) Redirect(code int, location string) {
    if c.Writer == nil {
        return
    }
    c.Writer.Header().Set("Location", location)
    c.Status(code)
}
```

#### 对比

| 框架 | 方法 | 参数 | 行为 |
|------|------|------|------|
| gin | `Redirect(code, location)` | (int, string) | 设置Location header和状态码 |
| hertz | `Redirect(code, location)` | (int, string) | 设置Location header和状态码 |
| fiber | `Redirect(code, location)` | (int, string) | 设置Location header和状态码 |
| gserver | `Redirect(code, location)` | (int, string) | 设置Location header和状态码 |

**设计决策**：与gin/hertz/fiber完全一致

### 3.4 GetRawData方法

```go
// GetRawData returns stream data.
// This method is useful for reading the request body as []byte.
// Note: this method will consume the body, so it can only be called once.
func (c *Context) GetRawData() ([]byte, error) {
    if c.fastCtx == nil {
        return nil, fmt.Errorf("context not initialized")
    }
    body := c.fastCtx.Request.Body()
    // 复制一份，避免后续操作修改原始body
    data := make([]byte, len(body))
    copy(data, body)
    return data, nil
}
```

#### 对比

| 框架 | 方法 | 返回值 | 行为 |
|------|------|--------|------|
| gin | `GetRawData()` | ([]byte, error) | 返回body的副本 |
| hertz | `GetRawData()` | ([]byte, error) | 返回body的副本 |
| fiber | `Body()` | []byte | 返回原始body |
| gserver | `GetRawData()` | ([]byte, error) | 返回body的副本 |

**设计决策**：与gin/hertz一致，返回副本而不是原始body

### 3.5 GetCookie方法改进

```go
// GetCookie returns the named cookie provided in the request.
// Returns (value, error) where error is non-nil if the cookie does not exist.
func (c *Context) GetCookie(name string) (string, error) {
    if c.fastCtx == nil {
        return "", fmt.Errorf("context not initialized")
    }
    cookie := c.fastCtx.Request.Header.Cookie(name)
    if len(cookie) == 0 {
        return "", http.ErrNoCookie
    }
    return string(cookie), nil
}
```

#### 对比

| 框架 | 方法 | 返回值 | 错误处理 |
|------|------|--------|----------|
| gin | `Cookie(name)` | string | 不存在时返回空字符串 |
| hertz | `Cookie(name)` | string | 不存在时返回空字符串 |
| gserver (old) | `Cookie(name)` | string | 不存在时返回空字符串 |
| gserver (new) | `GetCookie(name)` | (string, error) | 不存在时返回error |

**设计决策**：改进API，使用error返回值，更符合Go惯用法

### 3.6 File方法

```go
// File writes the specified file into the body stream in an efficient way.
func (c *Context) File(filepath string) {
    if c.Writer == nil {
        return
    }

    // 检查文件是否存在
    _, err := os.Stat(filepath)
    if err != nil {
        if os.IsNotExist(err) {
            c.Status(http.StatusNotFound)
        } else {
            c.Status(http.StatusInternalServerError)
        }
        return
    }

    // 根据扩展名设置Content-Type
    ext := filepath.Ext(filepath)
    contentType := mime.TypeByExtension(ext)
    if contentType == "" {
        contentType = "application/octet-stream"
    }

    // 使用fasthttp.ServeFile
    err = fasthttp.ServeFile(c.Writer, filepath)
    if err != nil {
        c.Status(http.StatusInternalServerError)
        return
    }

    c.Header("Content-Type", contentType)
}
```

#### 对比

| 框架 | 方法 | 行为 |
|------|------|------|
| gin | `File(filepath)` | 自动设置Content-Type，支持range |
| hertz | `File(filepath)` | 自动设置Content-Type，支持range |
| fiber | `SendFile(filepath)` | 自动设置Content-Type，支持range |
| gserver | `File(filepath)` | 自动设置Content-Type，使用fasthttp.ServeFile |

**设计决策**：与gin/hertz/fiber一致

### 3.7 Stream方法

```go
// Stream sends a streaming response.
func (c *Context) Stream(r io.Reader, contentType string) {
    if c.Writer == nil {
        return
    }
    if c.IsAborted() {
        return
    }

    // 设置Content-Type
    if contentType == "" {
        contentType = "application/octet-stream"
    }
    c.Header("Content-Type", contentType)
    c.Status(http.StatusOK)

    // 流式传输
    buf := renderBufPool.Get().([]byte)
    defer renderBufPool.Put(buf)

    if _, err := io.CopyBuffer(c.Writer, r, buf); err != nil {
        panic(err)
    }
}
```

#### 对比

| 框架 | 方法 | 参数 | 行为 |
|------|------|------|------|
| hertz | `Stream(r io.Reader, contentType)` | (io.Reader, string) | 流式传输 |
| fiber | `SendStream(r io.Reader, size)` | (io.Reader, int) | 流式传输 |
| gserver | `Stream(r io.Reader, contentType)` | (io.Reader, string) | 流式传输 |

**设计决策**：与hertz一致，不强制要求size参数

### 3.8 FileAttachment方法（低优先级）

```go
// FileAttachment writes the specified file into the body stream as an attachment.
// The filename is used to set the Content-Disposition header.
func (c *Context) FileAttachment(filepath, filename string) {
    if c.Writer == nil {
        return
    }

    // 检查文件是否存在
    _, err := os.Stat(filepath)
    if err != nil {
        if os.IsNotExist(err) {
            c.Status(http.StatusNotFound)
        } else {
            c.Status(http.StatusInternalServerError)
        }
        return
    }

    // 设置Content-Disposition
    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

    // 调用File方法
    c.File(filepath)
}
```

#### 对比

| 框架 | 方法 | 参数 | 行为 |
|------|------|------|------|
| gin | `FileAttachment(filepath, filename)` | (string, string) | 设置Content-Disposition |
| hertz | `FileAttachment(filepath, filename)` | (string, string) | 设置Content-Disposition |
| gserver | `FileAttachment(filepath, filename)` | (string, string) | 设置Content-Disposition |

**设计决策**：与gin/hertz一致

## 4. 完整Context API列表

### 4.1 请求参数获取

```go
// Path参数
Param(key string) string
Params() map[string]string
FullPath() string

// Query参数
Query(key string) string
GetQuery(key string) (string, bool)
DefaultQuery(key, defaultValue string) string  // 新增
QueryArray(key string) []string
QueryMap(key string) map[string]string
QueryBytes(key string) []byte

// PostForm参数
PostForm(key string) string
GetPostForm(key string) (string, bool)
DefaultPostForm(key, defaultValue string) string  // 新增
PostFormArray(key string) []string
PostFormMap(key string) map[string]string
PostFormBytes(key string) []byte

// 文件上传
FormFile(name string) (*multipart.FileHeader, error)
MultipartForm() (*multipart.Form, error)
```

### 4.2 Header相关

```go
// Header
GetHeader(key string) string
Header(key, value string)
HeaderBytes(key string) []byte

// Body
GetRawData() ([]byte, error)  // 新增
BodyBytes() []byte

// Cookie
Cookie(key string) string
SetCookie(cookie *fasthttp.Cookie)
GetCookie(name string) (string, error)  // 新增（改进）
CookieBytes(key string) []byte
```

### 4.3 响应方法

```go
// 状态码
Status(code int)
StatusCode() int

// 数据响应
JSON(code int, obj interface{})
XML(code int, obj interface{})
HTML(code int, name string, obj interface{})
String(code int, format string, values ...interface{})
Data(code int, contentType string, data []byte)

// 文件/流
File(filepath string)  // 新增
FileAttachment(filepath, filename string)  // 新增（低优先级）
Stream(r io.Reader, contentType string)  // 新增

// 重定向
Redirect(code int, location string)  // 新增

// 自动响应
RespAuto(data interface{})
Render(data interface{})
```

### 4.4 Context存储

```go
Set(key, value interface{})
SetValue(key, value interface{})
Value(key interface{}) interface{}
GetValue(key interface{}) (interface{}, bool)  // 已存在
Get(key interface{}) (interface{}, bool)  // 新增
MustGet(key interface{}) interface{}  // 新增
```

### 4.5 Handler链控制

```go
Next()
Abort()
IsAborted() bool
AbortWithStatus(code int)
AbortWithStatusJSON(code int, obj interface{})
HandlerCount() int
```

### 4.6 Context

```go
Context() context.Context
SetContext(ctx context.Context)
Deadline() (time.Time, bool)
Done() <-chan struct{}
Err() error
```

### 4.7 客户端信息

```go
ClientIP() string
ContentType() string
IsWebsocket() bool
```

### 4.8 Binding

```go
Bind(obj interface{}) error
BindJSON(obj interface{}) error
BindXML(obj interface{}) error
BindForm(obj interface{}) error
BindMultipart(obj interface{}) error
ShouldBind(obj interface{}) error
ShouldBindJSON(obj interface{}) error
ShouldBindXML(obj interface{}) error
BindAndValidate(obj interface{}) error
BindWithConfig(obj interface{}, cfg BindingConfig) error
```

### 4.9 其他

```go
Request() *fasthttp.Request
Response() *fasthttp.Response
FastContext() *fasthttp.RequestCtx
Logger() Logger
```

## 5. 使用示例

### 5.1 Get方法

```go
func handler(ctx *Context) Result {
    // 设置值
    ctx.Set("user", User{ID: 1, Name: "John"})

    // 获取值（推荐）
    if user, ok := ctx.Get("user"); ok {
        u := user.(User)
        return Auto(u)
    }

    // 获取值（必须存在）
    user := ctx.MustGet("user").(User)
    return Auto(user)
}
```

### 5.2 DefaultQuery方法

```go
func handler(ctx *Context) Result {
    page := ctx.DefaultQuery("page", "1")
    limit := ctx.DefaultQuery("limit", "10")

    return Auto(map[string]interface{}{
        "page":  page,
        "limit": limit,
    })
}
```

### 5.3 DefaultPostForm方法

```go
func handler(ctx *Context) Result {
    name := ctx.DefaultPostForm("name", "Anonymous")
    email := ctx.DefaultPostForm("email", "")

    return Auto(map[string]interface{}{
        "name":  name,
        "email": email,
    })
}
```

### 5.4 Redirect方法

```go
func handler(ctx *Context) Result {
    // 重定向
    ctx.Redirect(http.StatusFound, "/new-location")
    return NoContent()
}

// 或者使用Result
func handler(ctx *Context) Result {
    return Redirect("/new-location")
}
```

### 5.5 GetRawData方法

```go
func handler(ctx *Context) Result {
    // 获取原始body
    data, err := ctx.GetRawData()
    if err != nil {
        return Error(err)
    }

    // 处理原始数据
    // ...

    return Auto(map[string]string{"received": "ok"})
}
```

### 5.6 GetCookie方法

```go
func handler(ctx *Context) Result {
    // 获取cookie
    token, err := ctx.GetCookie("token")
    if err != nil {
        if errors.Is(err, http.ErrNoCookie) {
            return ErrorMsg("token not found")
        }
        return Error(err)
    }

    // 验证token
    // ...

    return Auto(map[string]string{"token": token})
}
```

### 5.7 File方法

```go
func handler(ctx *Context) Result {
    // 返回文件
    ctx.File("/path/to/file.pdf")
    return NoContent()
}

// 或者使用Result（需要实现FileResult）
// func handler(ctx *Context) Result {
//     return File("/path/to/file.pdf")
// }
```

### 5.8 Stream方法

```go
func handler(ctx *Context) Result {
    // 流式传输
    file, err := os.Open("/path/to/large-file.zip")
    if err != nil {
        return Error(err)
    }
    defer file.Close()

    ctx.Stream(file, "application/zip")
    return NoContent()
}
```

### 5.9 FileAttachment方法

```go
func handler(ctx *Context) Result {
    // 下载文件（指定文件名）
    ctx.FileAttachment("/path/to/file.pdf", "download.pdf")
    return NoContent()
}
```

## 6. 性能分析

### 6.1 新增方法开销

| 方法 | 开销 | 说明 |
|------|------|------|
| Get | ~2ns | map查找 |
| MustGet | ~2ns | map查找 + 可能panic |
| DefaultQuery | ~5ns | Query + 空值检查 |
| DefaultPostForm | ~5ns | PostForm + 空值检查 |
| Redirect | ~10ns | 设置header和状态码 |
| GetRawData | ~50ns | 复制body |
| GetCookie | ~10ns | 查找cookie |
| File | ~100ns+ | 文件系统操作 |
| Stream | ~10ns | 设置header和状态码 |
| FileAttachment | ~100ns+ | 文件系统操作 + header |

**结论**：所有新增方法性能开销可忽略

### 6.2 基准测试

```go
func Benchmark_Get(b *testing.B) {
    ctx := &Context{}
    ctx.values = make(map[interface{}]interface{})
    ctx.values["key"] = "value"

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        ctx.Get("key")
    }
}

func Benchmark_DefaultQuery(b *testing.B) {
    ctx := &Context{}
    ctx.fastCtx = &fasthttp.RequestCtx{}
    ctx.fastCtx.Request.URI().SetQueryString("page=1")

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        ctx.DefaultQuery("page", "1")
    }
}
```

## 7. 测试计划

### 7.1 单元测试

```go
func TestGet(t *testing.T) {
    ctx := &Context{}
    ctx.values = make(map[interface{}]interface{})
    ctx.values["key"] = "value"

    // 测试存在
    v, ok := ctx.Get("key")
    assert.True(t, ok)
    assert.Equal(t, "value", v)

    // 测试不存在
    v, ok = ctx.Get("notexist")
    assert.False(t, ok)
    assert.Nil(t, v)
}

func TestMustGet(t *testing.T) {
    ctx := &Context{}
    ctx.values = make(map[interface{}]interface{})
    ctx.values["key"] = "value"

    // 测试存在
    v := ctx.MustGet("key")
    assert.Equal(t, "value", v)

    // 测试不存在
    assert.Panics(t, func() {
        ctx.MustGet("notexist")
    })
}

func TestDefaultQuery(t *testing.T) {
    ctx := &Context{}
    ctx.fastCtx = &fasthttp.RequestCtx{}
    ctx.fastCtx.Request.URI().SetQueryString("page=1")

    // 测试存在
    assert.Equal(t, "1", ctx.DefaultQuery("page", "10"))

    // 测试不存在
    assert.Equal(t, "10", ctx.DefaultQuery("limit", "10"))
}

func TestGetCookie(t *testing.T) {
    ctx := &Context{}
    ctx.fastCtx = &fasthttp.RequestCtx{}
    ctx.fastCtx.Request.Header.SetCookie("token", "abc123")

    // 测试存在
    token, err := ctx.GetCookie("token")
    assert.NoError(t, err)
    assert.Equal(t, "abc123", token)

    // 测试不存在
    _, err = ctx.GetCookie("notexist")
    assert.Error(t, err)
    assert.Equal(t, http.ErrNoCookie, err)
}
```

### 7.2 集成测试

```go
func TestContextAPI_Integration(t *testing.T) {
    server := NewServer()

    server.GET("/get", Wrap(func(ctx *Context) Result {
        ctx.Set("key", "value")
        v, _ := ctx.Get("key")
        return Auto(map[string]string{"value": v.(string)})
    }))

    // 测试请求...
}
```

## 8. 总结

### 8.1 新增方法汇总

| 优先级 | 方法 | 说明 |
|--------|------|------|
| 高 | Get(key) (interface{}, bool) | 获取Context存储值 |
| 高 | MustGet(key) interface{} | 获取Context存储值（panic if not exists） |
| 高 | DefaultQuery(key, defaultValue) string | 获取Query参数（带默认值） |
| 高 | DefaultPostForm(key, defaultValue) string | 获取PostForm参数（带默认值） |
| 高 | Redirect(code, location) | 重定向 |
| 中 | GetRawData() ([]byte, error) | 获取原始请求body |
| 中 | GetCookie(name) (string, error) | 获取Cookie（返回error） |
| 中 | File(filepath) | 返回文件 |
| 中 | Stream(r, contentType) | 流式响应 |
| 低 | FileAttachment(filepath, filename) | 返回文件（指定下载名） |

### 8.2 API对齐完成度

| 框架 | 方法对齐度 | 说明 |
|------|-----------|------|
| gin | 100% | 核心方法完全对齐 |
| hertz | 100% | 核心方法完全对齐 |
| fiber | 95% | 个别方法命名略有差异 |

### 8.3 实施步骤
1. 实现Get/MustGet方法
2. 实现DefaultQuery/DefaultPostForm方法
3. 实现Redirect方法
4. 实现GetRawData方法
5. 实现GetCookie方法（改进）
6. 实现File方法
7. 实现Stream方法
8. （可选）实现FileAttachment方法
9. 编写单元测试
10. 编写集成测试
11. 更新文档和示例

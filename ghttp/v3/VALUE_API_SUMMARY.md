# v3 类型化值访问器 (Value API) 总结

**日期:** 2026-10-03  
**状态:** ✅ 完成并测试通过

---

## 🎯 设计目标

**问题：** 之前设计的类型化访问器需要定义结构体，过度工程化：

```go
// ❌ 旧设计：必须定义结构体
type UserID struct {
    ID int64 `path:"id"`
}

path, _ := v3.Path[UserID](&req).Get()
id := path.ID  // 还要再取字段
```

**新设计：** 直接获取基础类型，简单直观：

```go
// ✅ 新设计：直接获取基础类型
id, _ := req.PathValue("id").Int64()
page := req.QueryValue("page").IntOr(1)
token := req.HeaderValue("Authorization").String()
```

---

## ✅ 已实现功能

### 1. Value 访问器 (`value.go`)

支持所有常见类型的解析：

```go
type Value struct {
    raw string
    ok  bool  // 标记值是否存在
}

// 基础类型
String() string
Int() (int, error)
Int64() (int64, error)
Int32() (int32, error)
Uint() (uint, error)
Uint64() (uint64, error)
Bool() (bool, error)
Float64() (float64, error)
Float32() (float32, error)

// 带默认值（不返回 error）
StringOr(def string) string
IntOr(def int) int
Int64Or(def int64) int64
BoolOr(def bool) bool
Float64Or(def float64) float64
// ... 等等

// 辅助方法
Exists() bool
IsEmpty() bool
```

### 2. Values 多值访问器

用于 query 参数的多值场景：

```go
type Values struct {
    raw []string
}

Strings() []string
Ints() []int
Int64s() []int64
First() *Value
Join(sep string) string
Len() int
```

### 3. RequestInput 新方法

```go
// PathValue 返回 path 参数的类型化访问器
PathValue(name string) *Value

// QueryValue 返回 query 参数的类型化访问器
QueryValue(name string) *Value

// QueryValues 返回 query 参数的多值访问器
QueryValues(name string) *Values

// HeaderValue 返回 HTTP header 的类型化访问器
HeaderValue(name string) *Value

// CookieValue 返回 cookie 的类型化访问器
CookieValue(name string) *Value
```

**向后兼容：** 保留了旧的 API：
- `Path(name string) string`
- `QueryFirst(name string) (string, bool)`
- `QueryFirstValue(key string) string`

---

## 📊 测试结果

所有测试 100% 通过 ✅

### TestValueAPI
- ✅ PathValue - Int64 解析
- ✅ QueryValue - Int, Bool 解析
- ✅ QueryValues - 多值处理
- ✅ HeaderValue - 字符串访问
- ✅ DefaultValues - 默认值处理
- ✅ Exists - 存在性检查

### TestValueAPIRealHandler
- ✅ 真实 handler 场景

### TestValueAPIErrorHandling
- ✅ InvalidInt - 解析错误处理
- ✅ MissingRequired - 缺少参数错误
- ✅ WithDefaultNoError - 默认值无错误

### TestValueAPIComparison
- ✅ 新旧 API 对比

---

## 💡 使用示例

### 示例 1: 基础使用

```go
func GetUser(ctx context.Context, req v3.RequestOf[v3.NoData]) (User, error) {
    // 1. Path 参数（必需）
    userID, err := req.PathValue("id").Int64()
    if err != nil {
        return User{}, v3.HTTPError{Status: 400, Cause: err}
    }

    // 2. Query 参数（带默认值）
    page := req.QueryValue("page").IntOr(1)
    limit := req.QueryValue("limit").IntOr(20)

    // 3. Header
    authToken := req.HeaderValue("Authorization").String()
    if authToken == "" {
        return User{}, v3.HTTPError{Status: 401}
    }

    return db.GetUser(ctx, userID, page, limit)
}
```

### 示例 2: 提前返回优化

```go
func UpdateUser(ctx context.Context, req v3.RequestOf[UpdateBody]) (User, error) {
    // 1. 先检查 path 参数
    userID, err := req.PathValue("id").Int64()
    if err != nil {
        return User{}, v3.HTTPError{Status: 400, Cause: err}
    }

    // 2. 检查权限（提前返回）
    if !hasPermission(ctx, userID) {
        return User{}, v3.HTTPError{Status: 403}
        // ✅ Body 未解码，节省性能
    }

    // 3. 只在需要时才访问 Body
    body := req.Data

    return db.UpdateUser(ctx, userID, body)
}
```

### 示例 3: 多值处理

```go
func SearchUsers(ctx context.Context, req v3.RequestOf[v3.NoData]) ([]User, error) {
    // 单个 query 参数
    keyword := req.QueryValue("q").String()

    // 多值 query 参数
    tags := req.QueryValues("tags").Strings()  // ?tags=go&tags=web
    ids := req.QueryValues("ids").Int64s()     // ?ids=1&ids=2&ids=3

    // 分页参数
    page := req.QueryValue("page").IntOr(1)
    limit := req.QueryValue("limit").IntOr(20)

    return db.SearchUsers(ctx, keyword, tags, ids, page, limit)
}
```

### 示例 4: 错误处理

```go
func ProcessRequest(ctx context.Context, req v3.RequestOf[v3.NoData]) (Result, error) {
    // 必需参数
    id, err := req.PathValue("id").Int64()
    if err != nil {
        return Result{}, v3.HTTPError{
            Status: 400,
            Cause:  errors.New("invalid id: must be integer"),
        }
    }

    // 可选参数（使用默认值）
    timeout := req.QueryValue("timeout").IntOr(30)
    debug := req.QueryValue("debug").BoolOr(false)

    // 检查参数是否存在
    if req.QueryValue("force").Exists() {
        // force 参数存在，执行强制操作
    }

    return process(ctx, id, timeout, debug)
}
```

### 示例 5: 类型转换

```go
func Statistics(ctx context.Context, req v3.RequestOf[v3.NoData]) (Stats, error) {
    // 各种类型转换
    count := req.QueryValue("count").IntOr(100)
    price := req.QueryValue("price").Float64Or(0.0)
    active := req.QueryValue("active").BoolOr(true)
    category := req.QueryValue("category").StringOr("all")

    // 多种整数类型
    userID := req.PathValue("user_id").Int64Or(0)
    pageSize := req.QueryValue("page_size").Int32Or(20)
    offset := req.QueryValue("offset").UintOr(0)

    return calculateStats(count, price, active, category)
}
```

---

## 🔄 新旧 API 对比

### 旧 API（字符串）

```go
// ❌ 需要手动转换
idStr := req.Path("id")
id, err := strconv.ParseInt(idStr, 10, 64)
if err != nil {
    return User{}, errors.New("invalid id")
}

pageStr := req.QueryFirstValue("page")
page, _ := strconv.Atoi(pageStr)
if page == 0 {
    page = 1
}
```

### 新 API（类型安全）

```go
// ✅ 类型安全，自动转换
id, err := req.PathValue("id").Int64()
if err != nil {
    return User{}, v3.HTTPError{Status: 400, Cause: err}
}

page := req.QueryValue("page").IntOr(1)
```

---

## 📈 优势

### 1. 简单直观
- ✅ 无需定义结构体
- ✅ 直接获取基础类型
- ✅ API 清晰易懂

### 2. 类型安全
- ✅ 编译期类型检查
- ✅ 自动类型转换
- ✅ 错误处理明确

### 3. 高性能
- ✅ 无反射开销
- ✅ 零分配（基础类型）
- ✅ 直接解析

### 4. 向后兼容
- ✅ 保留旧 API
- ✅ 渐进式迁移
- ✅ 无破坏性变更

### 5. 灵活性
- ✅ 支持默认值
- ✅ 支持多值
- ✅ 支持存在性检查

---

## 🎯 与其他框架对比

### Gin

```go
// Gin
id := c.Param("id")  // string
page, _ := strconv.Atoi(c.Query("page"))

// v3
id, _ := req.PathValue("id").Int64()
page := req.QueryValue("page").IntOr(1)
```

### Echo

```go
// Echo
id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
page := c.QueryParam("page")

// v3
id, _ := req.PathValue("id").Int64()
page := req.QueryValue("page").IntOr(1)
```

### Fiber

```go
// Fiber
id, _ := c.ParamsInt("id")
page := c.QueryInt("page", 1)

// v3 (类似的简洁性)
id, _ := req.PathValue("id").Int64()
page := req.QueryValue("page").IntOr(1)
```

**v3 的优势：**
- ✅ 返回 `*Value` 提供更多灵活性
- ✅ 支持更多类型 (Int32, Uint64, Float32, etc.)
- ✅ 统一的错误处理
- ✅ 支持多值场景

---

## 🔗 相关文件

- `ghttp/v3/value.go` - Value 和 Values 类型定义
- `ghttp/v3/contracts.go` - RequestInput 新方法
- `ghttp/v3/value_test.go` - 完整测试用例

---

## 📝 下一步

基于这个新的 Value API，现在可以重新思考多格式 Body 访问器的设计：

### 可能的方向

1. **保持简单** - 不实现多格式访问器，保持 v3 当前的设计
   - 路由注册时通过 `WithInput()` 指定格式
   - handler 内部直接访问 `req.Data`
   - 用户可以直接访问 `req.Body` 进行自定义处理

2. **提供灵活性** - 添加可选的多格式支持
   - 提供 `req.BodyValue()` 返回类似的访问器
   - 支持动态选择解码器
   - 与当前的 `Value` API 风格一致

具体选择哪个方向，需要进一步讨论！

---

**总结：** Value API 实现完成，提供了简单、类型安全、高性能的参数访问方式。这是 v3 API 设计的重要改进！🎉

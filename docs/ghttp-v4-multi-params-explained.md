# ghttp v4 多参数处理详解

本文档详细解释 v4 如何处理**同时需要多种参数**的情况（如 path + body, query + body）。

---

## 问题场景

一个典型的 RESTful API 端点经常需要多种参数：

```
PATCH /users/:id
Authorization: Bearer <token>
Content-Type: application/json

{
  "username": "new_name",
  "email": "new@example.com"
}
```

这个请求包含：
- **路径参数**: `id`
- **请求头**: `Authorization`
- **请求体**: JSON `{username, email}`

如何在一个 handler 中同时获取这些参数？

---

## 方案对比

### v1 的方式（手动提取）

```go
func updateUser(ctx context.Context, req *Request, resp *Response) error {
    // 手动提取路径参数
    idStr := req.Params.Get("id")
    id, err := strconv.ParseInt(idStr, 10, 64)
    if err != nil {
        return resp.JSON(400, "invalid id")
    }
    
    // 手动提取 header
    token := req.Header.Get("Authorization")
    if token == "" {
        return resp.JSON(401, "unauthorized")
    }
    
    // 手动解析 JSON body
    var body UpdateUserRequest
    if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
        return resp.JSON(400, "invalid json")
    }
    
    // 业务逻辑
    user := updateUser(ctx, id, body)
    return resp.JSON(200, user)
}
```

**问题:**
- ❌ 大量样板代码
- ❌ 每个 handler 都要重复
- ❌ 无类型安全
- ❌ 错误处理分散

---

### v4 的方式（组合 Extractor）

```go
// 1. 定义参数组合
type UpdateUserParams struct {
    Path  v4.Path[struct{ ID int64 `path:"id"` }]
    Auth  v4.Header[struct{ Token string `header:"Authorization"` }]
    Body  v4.Json[UpdateUserRequest]
}

// 2. 实现 FromRequest（一次性）
func (p UpdateUserParams) FromRequest(ctx context.Context, r *v4.Request) (UpdateUserParams, error) {
    // 依次提取每个字段
    path, err := v4.Path[struct{ ID int64 `path:"id"` }]{}.FromRequest(ctx, r)
    if err != nil {
        return UpdateUserParams{}, err
    }
    
    auth, err := v4.Header[struct{ Token string `header:"Authorization"` }]{}.FromRequest(ctx, r)
    if err != nil {
        return UpdateUserParams{}, err
    }
    
    body, err := v4.Json[UpdateUserRequest]{}.FromRequest(ctx, r)
    if err != nil {
        return UpdateUserParams{}, err
    }
    
    return UpdateUserParams{
        Path: path,
        Auth: auth,
        Body: body,
    }, nil
}

// 3. Handler 非常简洁
func UpdateUser(ctx context.Context, params UpdateUserParams) (User, error) {
    id := params.Path.Value.ID
    token := params.Auth.Value.Token
    req := params.Body.Value
    
    // 验证 token
    if !validateToken(token) {
        return User{}, v4.Unauthorized("invalid token")
    }
    
    // 业务逻辑
    return db.UpdateUser(ctx, id, req)
}

// 4. 注册
v4.Patch("/users/:id", UpdateUser).MustRegister(server)
```

**优势:**
- ✅ 参数定义集中（UpdateUserParams）
- ✅ 提取逻辑复用（FromRequest 只写一次）
- ✅ Handler 只关注业务逻辑
- ✅ 完全类型安全
- ✅ 自动错误处理

---

## 深入理解：执行流程

### 请求到达时的完整流程

```
1. HTTP Request arrives
   PATCH /users/123
   Authorization: Bearer xyz
   Body: {"username": "john"}

2. v4 框架路由匹配
   匹配到: PATCH /users/:id → UpdateUser handler
   
3. v4 检查 UpdateUser 的输入类型
   In = UpdateUserParams
   
4. v4 检查 UpdateUserParams 是否实现 FromRequest
   ✅ 是的，有 FromRequest 方法
   
5. 调用 UpdateUserParams.FromRequest(ctx, request)
   
   5.1 提取 Path
       - 调用 Path[...]{}.FromRequest(ctx, request)
       - 从 request.Params.Get("id") → "123"
       - 解析为 int64: 123
       - 返回 Path{Value: {ID: 123}}
   
   5.2 提取 Auth
       - 调用 Header[...]{}.FromRequest(ctx, request)
       - 从 request.Header.Get("Authorization") → "Bearer xyz"
       - 返回 Header{Value: {Token: "Bearer xyz"}}
   
   5.3 提取 Body
       - 调用 Json[...]{}.FromRequest(ctx, request)
       - 从 request.Body 读取并 json.Decode
       - 返回 Json{Value: {Username: "john"}}
   
   5.4 组合结果
       return UpdateUserParams{
           Path: Path{Value: {ID: 123}},
           Auth: Header{Value: {Token: "Bearer xyz"}},
           Body: Json{Value: {Username: "john"}},
       }

6. 调用 UpdateUser(ctx, params)
   params.Path.Value.ID = 123
   params.Auth.Value.Token = "Bearer xyz"
   params.Body.Value.Username = "john"

7. Handler 执行业务逻辑并返回结果

8. v4 序列化结果为 JSON 响应
```

---

## 实际示例

### 示例 1: 简单场景（路径 + Body）

```go
// 场景: PATCH /articles/:id  更新文章
type UpdateArticleParams struct {
    Path v4.Path[struct{ ID int64 `path:"id"` }]
    Body v4.Json[struct {
        Title   string `json:"title"`
        Content string `json:"content"`
    }]
}

// 实现 FromRequest
func (p UpdateArticleParams) FromRequest(ctx context.Context, r *v4.Request) (UpdateArticleParams, error) {
    path, err := v4.Path[struct{ ID int64 `path:"id"` }]{}.FromRequest(ctx, r)
    if err != nil {
        return UpdateArticleParams{}, err
    }
    
    body, err := v4.Json[struct {
        Title   string `json:"title"`
        Content string `json:"content"`
    }]{}.FromRequest(ctx, r)
    if err != nil {
        return UpdateArticleParams{}, err
    }
    
    return UpdateArticleParams{Path: path, Body: body}, nil
}

func UpdateArticle(ctx context.Context, params UpdateArticleParams) (Article, error) {
    return db.UpdateArticle(
        ctx,
        params.Path.Value.ID,
        params.Body.Value.Title,
        params.Body.Value.Content,
    )
}
```

---

### 示例 2: 复杂场景（认证 + 路径 + Query + Body）

```go
// 场景: POST /users/:id/comments?notify=true  添加评论
type AddCommentParams struct {
    User  CurrentUser                                    // 自定义 extractor
    Path  v4.Path[struct{ UserID int64 `path:"id"` }]
    Query v4.Query[struct{ Notify bool `query:"notify"` }]
    Body  v4.Json[struct{ Content string `json:"content"` }]
}

func (p AddCommentParams) FromRequest(ctx context.Context, r *v4.Request) (AddCommentParams, error) {
    // 1. 提取当前用户（自动验证 token）
    user, err := CurrentUser{}.FromRequest(ctx, r)
    if err != nil {
        return AddCommentParams{}, err  // 401 Unauthorized
    }
    
    // 2. 提取路径参数
    path, err := v4.Path[struct{ UserID int64 `path:"id"` }]{}.FromRequest(ctx, r)
    if err != nil {
        return AddCommentParams{}, err  // 400 Bad Request
    }
    
    // 3. 提取查询参数
    query, err := v4.Query[struct{ Notify bool `query:"notify"` }]{}.FromRequest(ctx, r)
    if err != nil {
        return AddCommentParams{}, err  // 400 Bad Request
    }
    
    // 4. 提取 JSON body
    body, err := v4.Json[struct{ Content string `json:"content"` }]{}.FromRequest(ctx, r)
    if err != nil {
        return AddCommentParams{}, err  // 400 Bad Request
    }
    
    return AddCommentParams{
        User:  user,
        Path:  path,
        Query: query,
        Body:  body,
    }, nil
}

func AddComment(ctx context.Context, params AddCommentParams) (Comment, error) {
    comment := Comment{
        AuthorID: params.User.ID,
        TargetID: params.Path.Value.UserID,
        Content:  params.Body.Value.Content,
    }
    
    if err := db.SaveComment(ctx, &comment); err != nil {
        return Comment{}, err
    }
    
    // 如果需要通知
    if params.Query.Value.Notify {
        notifyUser(params.Path.Value.UserID, comment)
    }
    
    return comment, nil
}
```

---

## 优化：自动组合 Extractor

**问题:** 手动写 `FromRequest` 有点繁琐。

**解决:** 框架可以**自动识别**结构体字段并提取。

### 实现思路

```go
// 在 contracts.go 中添加通用的组合提取器

// autoExtractStruct 自动提取结构体的每个字段
func autoExtractStruct[T any](ctx context.Context, r *Request) (T, error) {
    var result T
    resultValue := reflect.ValueOf(&result).Elem()
    typ := reflect.TypeFor[T]()
    
    for i := 0; i < typ.NumField(); i++ {
        field := typ.Field(i)
        fieldValue := resultValue.Field(i)
        
        // 检查字段类型是否实现 FromRequest
        if !implementsFromRequest(field.Type) {
            continue  // 跳过不支持的字段
        }
        
        // 创建字段类型的零值
        extractor := reflect.New(field.Type).Elem().Interface()
        
        // 调用 FromRequest（通过反射）
        method := reflect.ValueOf(extractor).MethodByName("FromRequest")
        results := method.Call([]reflect.Value{
            reflect.ValueOf(ctx),
            reflect.ValueOf(r),
        })
        
        // 检查错误
        if err := results[1].Interface(); err != nil {
            return result, err.(error)
        }
        
        // 设置字段值
        fieldValue.Set(results[0])
    }
    
    return result, nil
}

// 辅助函数: 检查类型是否实现 FromRequest
func implementsFromRequest(typ reflect.Type) bool {
    // 检查是否有 FromRequest 方法
    method, ok := typ.MethodByName("FromRequest")
    if !ok {
        return false
    }
    
    // 检查方法签名
    // func (T) FromRequest(context.Context, *Request) (T, error)
    if method.Type.NumIn() != 3 || method.Type.NumOut() != 2 {
        return false
    }
    
    return true
}
```

### 使用自动组合

```go
// 用户只需定义结构体
type UpdateUserParams struct {
    Path v4.Path[UserID]
    Body v4.Json[UpdateBody]
}

// FromRequest 自动实现！
func (p UpdateUserParams) FromRequest(ctx context.Context, r *v4.Request) (UpdateUserParams, error) {
    return autoExtractStruct[UpdateUserParams](ctx, r)
}

// 或者更简洁: 使用 embedding
type UpdateUserParams struct {
    v4.AutoExtract  // 嵌入自动提取功能
    
    Path v4.Path[UserID]
    Body v4.Json[UpdateBody]
}

// 现在 UpdateUserParams 自动有了 FromRequest 方法！
```

---

## 性能考虑

### 手动组合 vs 自动组合

| 方案 | 性能 | 开发体验 |
|------|------|---------|
| **手动组合** | ⚡⚡⚡ 最快（无额外反射） | ⚠️ 需要手写 FromRequest |
| **自动组合** | ⚡⚡ 快（注册期缓存） | ✅ 零样板代码 |

### 优化自动组合的性能

```go
// 缓存编译结果
var structExtractors = make(map[reflect.Type]func(context.Context, *Request) (any, error))

func getOrCompileStructExtractor[T any]() func(context.Context, *Request) (T, error) {
    typ := reflect.TypeFor[T]()
    
    if cached, ok := structExtractors[typ]; ok {
        return func(ctx context.Context, r *Request) (T, error) {
            result, err := cached(ctx, r)
            return result.(T), err
        }
    }
    
    // 编译一次
    extractor := compileStructExtractor[T]()
    structExtractors[typ] = func(ctx context.Context, r *Request) (any, error) {
        return extractor(ctx, r)
    }
    
    return extractor
}

func compileStructExtractor[T any]() func(context.Context, *Request) (T, error) {
    typ := reflect.TypeFor[T]()
    
    // 分析每个字段，构建提取计划
    type fieldPlan struct {
        index     int
        extractor func(context.Context, *Request) (any, error)
    }
    
    var plans []fieldPlan
    for i := 0; i < typ.NumField(); i++ {
        field := typ.Field(i)
        if implementsFromRequest(field.Type) {
            plans = append(plans, fieldPlan{
                index:     i,
                extractor: compileFieldExtractor(field.Type),
            })
        }
    }
    
    // 返回优化的提取器
    return func(ctx context.Context, r *Request) (T, error) {
        var result T
        resultValue := reflect.ValueOf(&result).Elem()
        
        for _, plan := range plans {
            value, err := plan.extractor(ctx, r)
            if err != nil {
                return result, err
            }
            resultValue.Field(plan.index).Set(reflect.ValueOf(value))
        }
        
        return result, nil
    }
}
```

**优化效果:**
- ✅ 每种类型只分析一次
- ✅ 运行期直接调用编译好的提取器
- ✅ 性能接近手动组合

---

## 最佳实践

### 1. 参数命名约定

```go
type <Action><Resource>Params struct {
    User  CurrentUser           // 认证用户（如果需要）
    Path  v4.Path[...]          // 路径参数
    Query v4.Query[...]         // 查询参数
    Body  v4.Json[...] 或 Form  // 请求体
}
```

### 2. 复用参数定义

```go
// 定义可复用的类型
type UserIDPath struct {
    ID int64 `path:"id"`
}

type PaginationQuery struct {
    Page     int `query:"page" default:"1"`
    PageSize int `query:"page_size" default:"20"`
}

// 在多个端点复用
type ListUsersParams struct {
    Query v4.Query[PaginationQuery]
}

type ListPostsParams struct {
    Query v4.Query[PaginationQuery]
}

type GetUserParams struct {
    Path v4.Path[UserIDPath]
}

type UpdateUserParams struct {
    Path v4.Path[UserIDPath]
    Body v4.Json[UpdateUserRequest]
}
```

### 3. 分层验证

```go
type CreateUserParams struct {
    Body v4.Json[CreateUserRequest]
}

// 在 FromRequest 中做基本验证
func (p CreateUserParams) FromRequest(ctx context.Context, r *v4.Request) (CreateUserParams, error) {
    body, err := v4.Json[CreateUserRequest]{}.FromRequest(ctx, r)
    if err != nil {
        return CreateUserParams{}, err
    }
    
    // 额外的业务验证
    if body.Value.Age < 18 {
        return CreateUserParams{}, v4.BadRequest("must be 18+")
    }
    
    return CreateUserParams{Body: body}, nil
}

// Handler 只关注核心逻辑
func CreateUser(ctx context.Context, params CreateUserParams) (User, error) {
    // params.Body.Value 已经验证过了
    return db.CreateUser(ctx, params.Body.Value)
}
```

---

## 总结

v4 处理多参数的核心思想:

1. **组合优于继承**
   - 定义组合结构体（如 UpdateUserParams）
   - 包含所需的所有 extractor（Path, Query, Body 等）

2. **FromRequest 作为组合点**
   - 依次调用每个字段的 FromRequest
   - 任一提取失败，整个请求失败
   - 错误自动传播

3. **Handler 保持简洁**
   - 单个参数（组合结构体）
   - 所有数据已提取和验证
   - 只关注业务逻辑

4. **可选的自动化**
   - 框架可以自动组合
   - 用户零样板代码
   - 性能通过缓存优化

这个设计在**类型安全、灵活性和性能**之间取得了很好的平衡！

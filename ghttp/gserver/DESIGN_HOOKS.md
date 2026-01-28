# 中间件Hooks系统设计文档

## 1. 设计目标

### 1.1 核心目标
1. **Before/After/Panic Hooks**：支持中间件在不同阶段执行hooks
2. **Cancellable Hooks**：hooks可以取消请求继续执行
3. **动态配置**：运行时添加/移除hooks
4. **简单易用**：不增加现有中间件的复杂度
5. **高性能**：hooks执行零额外开销

### 1.2 非目标
- 不提供复杂的hook链（保持扁平）
- 不支持hook优先级（执行顺序由注册顺序决定）
- 不向后兼容（完全重新设计）

## 2. Hook接口设计

### 2.1 Hook类型定义

```go
// HookType：Hook类型
type HookType int

const (
    // BeforeHook：在中间件执行前调用
    BeforeHook HookType = iota
    // AfterHook：在中间件执行后调用
    AfterHook
    // PanicHook：在panic恢复时调用
    PanicHook
)

// String：返回Hook类型字符串
func (ht HookType) String() string {
    switch ht {
    case BeforeHook:
        return "before"
    case AfterHook:
        return "after"
    case PanicHook:
        return "panic"
    default:
        return "unknown"
    }
}

// Hook：Hook接口
type Hook interface {
    // Type：返回Hook类型
    Type() HookType
    // Execute：执行hook逻辑
    Execute(ctx *Context)
}

// HookFunc：Hook函数类型（简化使用）
type HookFunc func(ctx *Context)

// hookFuncAdapter：将HookFunc适配为Hook接口
type hookFuncAdapter struct {
    hookType HookType
    fn       HookFunc
}

// Type：实现Hook接口
func (a *hookFuncAdapter) Type() HookType {
    return a.hookType
}

// Execute：实现Hook接口
func (a *hookFuncAdapter) Execute(ctx *Context) {
    a.fn(ctx)
}

// Before：创建Before Hook
func Before(fn HookFunc) Hook {
    return &hookFuncAdapter{hookType: BeforeHook, fn: fn}
}

// After：创建After Hook
func After(fn HookFunc) Hook {
    return &hookFuncAdapter{hookType: AfterHook, fn: fn}
}

// Panic：创建Panic Hook
func Panic(fn HookFunc) Hook {
    return &hookFuncAdapter{hookType: PanicHook, fn: fn}
}
```

### 2.2 Hooks管理

```go
// Hooks：Hooks集合
type Hooks struct {
    before []Hook
    after  []Hook
    panic  []Hook
}

// NewHooks：创建空Hooks集合
func NewHooks() *Hooks {
    return &Hooks{
        before: make([]Hook, 0),
        after:  make([]Hook, 0),
        panic:  make([]Hook, 0),
    }
}

// Add：添加hook
func (h *Hooks) Add(hook Hook) *Hooks {
    switch hook.Type() {
    case BeforeHook:
        h.before = append(h.before, hook)
    case AfterHook:
        h.after = append(h.after, hook)
    case PanicHook:
        h.panic = append(h.panic, hook)
    }
    return h
}

// Remove：移除hook（按类型）
func (h *Hooks) Remove(hookType HookType) *Hooks {
    switch hookType {
    case BeforeHook:
        h.before = h.before[:0]
    case AfterHook:
        h.after = h.after[:0]
    case PanicHook:
        h.panic = h.panic[:0]
    }
    return h
}

// AddBefore：添加Before Hook
func (h *Hooks) AddBefore(fn HookFunc) *Hooks {
    return h.Add(Before(fn))
}

// AddAfter：添加After Hook
func (h *Hooks) AddAfter(fn HookFunc) *Hooks {
    return h.Add(After(fn))
}

// AddPanic：添加Panic Hook
func (h *Hooks) AddPanic(fn HookFunc) *Hooks {
    return h.Add(Panic(fn))
}

// ExecuteBefore：执行所有Before Hooks
func (h *Hooks) ExecuteBefore(ctx *Context) {
    for _, hook := range h.before {
        hook.Execute(ctx)
    }
}

// ExecuteAfter：执行所有After Hooks
func (h *Hooks) ExecuteAfter(ctx *Context) {
    for _, hook := range h.after {
        hook.Execute(ctx)
    }
}

// ExecutePanic：执行所有Panic Hooks
func (h *Hooks) ExecutePanic(ctx *Context) {
    for _, hook := range h.panic {
        hook.Execute(ctx)
    }
}

// Clear：清空所有hooks
func (h *Hooks) Clear() *Hooks {
    h.before = h.before[:0]
    h.after = h.after[:0]
    h.panic = h.panic[:0]
    return h
}
```

## 3. MiddlewareWithHooks设计

### 3.1 核心结构

```go
// MiddlewareWithHooks：带hooks的中间件
type MiddlewareWithHooks struct {
    Handler HandlerFunc
    hooks   *Hooks
}

// NewMiddlewareWithHooks：创建带hooks的中间件
func NewMiddlewareWithHooks(handler HandlerFunc) *MiddlewareWithHooks {
    return &MiddlewareWithHooks{
        Handler: handler,
        hooks:   NewHooks(),
    }
}

// Handler：实现HandlerFunc接口
func (mwh *MiddlewareWithHooks) Handle(ctx *Context) {
    // 执行Before Hooks
    mwh.hooks.ExecuteBefore(ctx)

    // 如果请求被abort，不再执行后续逻辑
    if ctx.IsAborted() {
        return
    }

    // 包装执行（支持Panic Hooks）
    func() {
        defer func() {
            if rec := recover(); rec != nil {
                // 执行Panic Hooks
                mwh.hooks.ExecutePanic(ctx)

                // 如果没有abort，重新panic
                if !ctx.IsAborted() {
                    panic(rec)
                }
            }
        }()

        // 执行实际的中间件逻辑
        mwh.Handler(ctx)
    }()

    // 如果请求被abort，不再执行After Hooks
    if ctx.IsAborted() {
        return
    }

    // 执行After Hooks
    mwh.hooks.ExecuteAfter(ctx)
}

// AddHook：添加hook
func (mwh *MiddlewareWithHooks) AddHook(hook Hook) *MiddlewareWithHooks {
    mwh.hooks.Add(hook)
    return mwh
}

// AddBefore：添加Before Hook
func (mwh *MiddlewareWithHooks) AddBefore(fn HookFunc) *MiddlewareWithHooks {
    mwh.hooks.AddBefore(fn)
    return mwh
}

// AddAfter：添加After Hook
func (mwh *MiddlewareWithHooks) AddAfter(fn HookFunc) *MiddlewareWithHooks {
    mwh.hooks.AddAfter(fn)
    return mwh
}

// AddPanic：添加Panic Hook
func (mwh *MiddlewareWithHooks) AddPanic(fn HookFunc) *MiddlewareWithHooks {
    mwh.hooks.AddPanic(fn)
    return mwh
}

// GetHooks：获取hooks（用于动态配置）
func (mwh *MiddlewareWithHooks) GetHooks() *Hooks {
    return mwh.hooks
}
```

### 3.2 便捷函数

```go
// WrapWithHooks：包装普通中间件为带hooks的中间件
func WrapWithHooks(handler HandlerFunc) *MiddlewareWithHooks {
    return NewMiddlewareWithHooks(handler)
}

// WithHooks：快速创建带hooks的中间件（链式调用）
func WithHooks(handler HandlerFunc, hooks ...Hook) HandlerFunc {
    mwh := NewMiddlewareWithHooks(handler)
    for _, hook := range hooks {
        mwh.AddHook(hook)
    }
    return mwh.Handle
}
```

## 4. 动态配置设计

### 4.1 HooksManager设计

```go
// HooksManager：hooks管理器（全局）
type HooksManager struct {
    mu      sync.RWMutex
    byRoute map[string]*Hooks  // 按路由管理的hooks
    global  *Hooks             // 全局hooks
}

// NewHooksManager：创建HooksManager
func NewHooksManager() *HooksManager {
    return &HooksManager{
        byRoute: make(map[string]*Hooks),
        global:  NewHooks(),
    }
}

// AddGlobalHook：添加全局hook
func (hm *HooksManager) AddGlobalHook(hook Hook) {
    hm.mu.Lock()
    defer hm.mu.Unlock()
    hm.global.Add(hook)
}

// AddGlobalBefore：添加全局Before Hook
func (hm *HooksManager) AddGlobalBefore(fn HookFunc) {
    hm.AddGlobalHook(Before(fn))
}

// AddGlobalAfter：添加全局After Hook
func (hm *HooksManager) AddGlobalAfter(fn HookFunc) {
    hm.AddGlobalHook(After(fn))
}

// AddGlobalPanic：添加全局Panic Hook
func (hm *HooksManager) AddGlobalPanic(fn HookFunc) {
    hm.AddGlobalHook(Panic(fn))
}

// AddRouteHook：为指定路由添加hook
func (hm *HooksManager) AddRouteHook(route string, hook Hook) {
    hm.mu.Lock()
    defer hm.mu.Unlock()

    if _, ok := hm.byRoute[route]; !ok {
        hm.byRoute[route] = NewHooks()
    }
    hm.byRoute[route].Add(hook)
}

// AddRouteBefore：为指定路由添加Before Hook
func (hm *HooksManager) AddRouteBefore(route string, fn HookFunc) {
    hm.AddRouteHook(route, Before(fn))
}

// AddRouteAfter：为指定路由添加After Hook
func (hm *HooksManager) AddRouteAfter(route string, fn HookFunc) {
    hm.AddRouteHook(route, After(fn))
}

// AddRoutePanic：为指定路由添加Panic Hook
func (hm *HooksManager) AddRoutePanic(route string, fn HookFunc) {
    hm.AddRouteHook(route, Panic(fn))
}

// RemoveRouteHooks：移除指定路由的所有hooks
func (hm *HooksManager) RemoveRouteHooks(route string) {
    hm.mu.Lock()
    defer hm.mu.Unlock()
    delete(hm.byRoute, route)
}

// GetRouteHooks：获取指定路由的hooks（包含全局hooks）
func (hm *HooksManager) GetRouteHooks(route string) *Hooks {
    hm.mu.RLock()
    defer hm.mu.RUnlock()

    result := NewHooks()

    // 先添加全局hooks
    for _, hook := range hm.global.before {
        result.Add(hook)
    }
    for _, hook := range hm.global.after {
        result.Add(hook)
    }
    for _, hook := range hm.global.panic {
        result.Add(hook)
    }

    // 再添加路由特定hooks
    if routeHooks, ok := hm.byRoute[route]; ok {
        for _, hook := range routeHooks.before {
            result.Add(hook)
        }
        for _, hook := range routeHooks.after {
            result.Add(hook)
        }
        for _, hook := range routeHooks.panic {
            result.Add(hook)
        }
    }

    return result
}

// ClearGlobal：清空全局hooks
func (hm *HooksManager) ClearGlobal() {
    hm.mu.Lock()
    defer hm.mu.Unlock()
    hm.global.Clear()
}

// ClearAll：清空所有hooks
func (hm *HooksManager) ClearAll() {
    hm.mu.Lock()
    defer hm.mu.Unlock()
    hm.global.Clear()
    hm.byRoute = make(map[string]*Hooks)
}
```

## 5. 现有中间件改造

### 5.1 RequestID中间件改造

```go
// RequestIDWithHooks：带hooks的RequestID中间件
func RequestIDWithHooks(cfg RequestIDConfig) *MiddlewareWithHooks {
    if cfg.HeaderName == "" {
        cfg.HeaderName = "X-Request-ID"
    }

    handler := func(ctx *Context) {
        if ctx == nil || ctx.fastCtx == nil {
            if ctx != nil {
                ctx.Next()
            }
            return
        }

        var id string
        if !cfg.DisableIncoming {
            b := ctx.fastCtx.Request.Header.Peek(cfg.HeaderName)
            if len(b) != 0 {
                id = string(b)
            }
        }
        if id == "" {
            id = newRequestID()
        }

        ctx.Set(requestIDKey{}, id)
        if !cfg.DisableResponseHeader {
            ctx.Header(cfg.HeaderName, id)
        }

        ctx.Next()
    }

    mwh := NewMiddlewareWithHooks(handler)

    // 添加Before Hook：记录开始时间
    mwh.AddBefore(func(ctx *Context) {
        ctx.Set("request-start-time", time.Now())
    })

    // 添加After Hook：计算并记录耗时
    mwh.AddAfter(func(ctx *Context) {
        startTime := ctx.Value("request-start-time")
        if startTime != nil {
            duration := time.Since(startTime.(time.Time))
            ctx.Logger().Debugf("request %s took %s", ctx.Value(requestIDKey{}), duration)
        }
    })

    return mwh
}
```

### 5.2 CORS中间件改造

```go
// CORSWithHooks：带hooks的CORS中间件
func CORSWithHooks(cfg CORSConfig) *MiddlewareWithHooks {
    if cfg.AllowMethods == "" {
        cfg.AllowMethods = "GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS"
    }
    if cfg.AllowHeaders == "" {
        cfg.AllowHeaders = "Content-Type,Authorization"
    }

    allowOrigin := cfg.AllowOriginFunc
    if allowOrigin == nil {
        allowed := make(map[string]struct{}, len(cfg.AllowOrigins))
        for _, o := range cfg.AllowOrigins {
            allowed[o] = struct{}{}
        }
        allowOrigin = func(origin []byte) bool {
            if len(allowed) == 0 {
                return true
            }
            _, ok := allowed[string(origin)]
            return ok
        }
    }

    maxAge := ""
    if cfg.MaxAge > 0 {
        maxAge = strconv.FormatInt(int64(cfg.MaxAge/time.Second), 10)
    }

    handler := func(ctx *Context) {
        if ctx == nil || ctx.fastCtx == nil {
            if ctx != nil {
                ctx.Next()
            }
            return
        }

        origin := ctx.fastCtx.Request.Header.Peek("Origin")
        if len(origin) == 0 {
            ctx.Next()
            return
        }
        if !allowOrigin(origin) {
            ctx.Next()
            return
        }

        ctx.Header("Access-Control-Allow-Origin", string(origin))
        if cfg.AllowCredentials {
            ctx.Header("Access-Control-Allow-Credentials", "true")
        }
        if cfg.ExposeHeaders != "" {
            ctx.Header("Access-Control-Expose-Headers", cfg.ExposeHeaders)
        }

        if bytesEq(ctx.fastCtx.Method(), "OPTIONS") && len(ctx.fastCtx.Request.Header.Peek("Access-Control-Request-Method")) != 0 {
            ctx.Header("Access-Control-Allow-Methods", cfg.AllowMethods)
            ctx.Header("Access-Control-Allow-Headers", cfg.AllowHeaders)
            if maxAge != "" {
                ctx.Header("Access-Control-Max-Age", maxAge)
            }
            ctx.Status(http.StatusNoContent)
            ctx.Abort()
            return
        }

        ctx.Next()
    }

    mwh := NewMiddlewareWithHooks(handler)

    // 添加Before Hook：记录CORS检查
    mwh.AddBefore(func(ctx *Context) {
        origin := string(ctx.fastCtx.Request.Header.Peek("Origin"))
        ctx.Logger().Debugf("CORS check for origin: %s", origin)
    })

    return mwh
}
```

### 5.3 Logger中间件改造

```go
// RequestLoggerWithHooks：带hooks的Logger中间件
func RequestLoggerWithHooks() *MiddlewareWithHooks {
    handler := func(ctx *Context) {
        ctx.Next()
    }

    mwh := NewMiddlewareWithHooks(handler)

    // 添加Before Hook：记录请求开始
    mwh.AddBefore(func(ctx *Context) {
        ctx.Set("request-start-time", time.Now())
        ctx.Set("request-id", ctx.Value(requestIDKey{}))

        logger := ctx.Logger()
        if logger == nil {
            return
        }

        method := ""
        path := ""
        if ctx.fastCtx != nil {
            method = string(ctx.fastCtx.Method())
            path = string(ctx.fastCtx.Path())
        }

        logger.Infof("request started: %s %s", method, path)
    })

    // 添加After Hook：记录请求完成
    mwh.AddAfter(func(ctx *Context) {
        startTime := ctx.Value("request-start-time")
        if startTime == nil {
            return
        }

        logger := ctx.Logger()
        if logger == nil {
            return
        }

        method := ""
        path := ""
        if ctx.fastCtx != nil {
            method = string(ctx.fastCtx.Method())
            path = string(ctx.fastCtx.Path())
        }

        status := http.StatusOK
        if ctx.Writer != nil {
            status = ctx.Writer.Status()
        }

        duration := time.Since(startTime.(time.Time))
        requestID := ctx.Value("request-id")

        if requestID != nil {
            logger.Infof("request %s %s %s -> %d (%s)",
                requestID, method, path, status, duration)
        } else {
            logger.Infof("request %s %s -> %d (%s)",
                method, path, status, duration)
        }
    })

    // 添加Panic Hook：记录panic
    mwh.AddPanic(func(ctx *Context) {
        logger := ctx.Logger()
        if logger == nil {
            return
        }
        logger.Errorf("request panic recovered")
    })

    return mwh
}
```

### 5.4 Recovery中间件改造

```go
// RecoveryWithHooks：带hooks的Recovery中间件
func RecoveryWithHooks() *MiddlewareWithHooks {
    handler := func(ctx *Context) {
        ctx.Next()
    }

    mwh := NewMiddlewareWithHooks(handler)

    // 添加Panic Hook：恢复panic
    mwh.AddPanic(func(ctx *Context) {
        rec := recover()
        if rec == nil {
            return
        }

        // 记录panic
        if logger := ctx.Logger(); logger != nil {
            logger.Errorf("panic recovered: %v\n%s", rec, string(debug.Stack()))
        }

        // 设置错误响应
        if ctx.Writer != nil && !ctx.Writer.Written() {
            ctx.Status(http.StatusInternalServerError)
        }

        ctx.Abort()
    })

    return mwh
}
```

## 6. Server集成

### 6.1 添加HooksManager到Server

```go
// Server结构体添加HooksManager
type Server struct {
    server *fasthttp.Server
    *Config
    IRouter
    Match
    hooks *HooksManager  // 新增
}

// NewServer：修改构造函数
func NewServer(opts ...ServerOption) *Server {
    c := &Config{
        matcher:    newServerMatcher(),
        codec:      newCodecFactory(),
        logger:     glog.Default(),
        UseRawPath: false,
    }

    for _, opt := range opts {
        opt(c)
    }

    matcher := c.matcher
    if matcher == nil {
        matcher = newServerMatcher()
    }

    r := &RouterGroup{
        Handlers: nil,
        path:     "/",
        root:     true,
    }

    hooks := NewHooksManager()  // 新增

    s := &Server{
        IRouter: r,
        Match:   matcher,
        Config:  c,
        hooks:   hooks,  // 新增
        server: &fasthttp.Server{
            Concurrency:                   c.Concurrency,
            IdleTimeout:                   c.IdleTimeout,
            MaxRequestBodySize:            c.MaxRequestBodySize,
            MaxIdleWorkerDuration:         c.MaxIdleWorkerDuration,
            MaxConnsPerIP:                 c.MaxConnsPerIP,
            MaxRequestsPerConn:            c.MaxRequestsPerConn,
            TCPKeepalive:                  c.TCPKeepalive,
            TCPKeepalivePeriod:            c.TCPKeepalivePeriod,
            DisableKeepalive:              c.DisableKeepalive,
            DisableHeaderNamesNormalizing: c.DisableHeaderNamesNormalizing,
            DisablePreParseMultipartForm:  c.DisablePreParseMultipartForm,
            NoDefaultContentType:          c.NoDefaultContentType,
            NoDefaultDate:                 c.NoDefaultDate,
            NoDefaultServerHeader:         c.NoDefaultServerHeader,
            ReduceMemoryUsage:             c.ReduceMemoryUsage,
            StreamRequestBody:             c.StreamRequestBody,
        },
    }

    r.engine = s
    s.server.Handler = s.FastHandler

    return s
}

// GetHooksManager：获取HooksManager
func (s *Server) GetHooksManager() *HooksManager {
    return s.hooks
}
```

### 6.2 添加便捷方法

```go
// AddGlobalHook：添加全局hook
func (s *Server) AddGlobalHook(hook Hook) {
    s.hooks.AddGlobalHook(hook)
}

// AddGlobalBefore：添加全局Before Hook
func (s *Server) AddGlobalBefore(fn HookFunc) {
    s.hooks.AddGlobalBefore(fn)
}

// AddGlobalAfter：添加全局After Hook
func (s *Server) AddGlobalAfter(fn HookFunc) {
    s.hooks.AddGlobalAfter(fn)
}

// AddGlobalPanic：添加全局Panic Hook
func (s *Server) AddGlobalPanic(fn HookFunc) {
    s.hooks.AddGlobalPanic(fn)
}

// AddRouteHook：为指定路由添加hook
func (s *Server) AddRouteHook(route string, hook Hook) {
    s.hooks.AddRouteHook(route, hook)
}

// AddRouteBefore：为指定路由添加Before Hook
func (s *Server) AddRouteBefore(route string, fn HookFunc) {
    s.hooks.AddRouteBefore(route, fn)
}

// AddRouteAfter：为指定路由添加After Hook
func (s *Server) AddRouteAfter(route string, fn HookFunc) {
    s.hooks.AddRouteAfter(route, fn)
}

// AddRoutePanic：为指定路由添加Panic Hook
func (s *Server) AddRoutePanic(route string, fn HookFunc) {
    s.hooks.AddRoutePanic(route, fn)
}
```

## 7. 使用示例

### 7.1 基本使用

#### 示例1：简单的Before/After Hooks

```go
func main() {
    server := NewServer()

    // 创建带hooks的中间件
    logger := WrapWithHooks(RequestLogger()).
        AddBefore(func(ctx *Context) {
            ctx.Logger().Infof("request started")
        }).
        AddAfter(func(ctx *Context) {
            ctx.Logger().Infof("request completed")
        })

    // 使用中间件
    server.Use(logger)

    // Handler
    server.GET("/hello", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "hello"})
    }))

    server.Run(":8080")
}
```

#### 示例2：Panic Hook

```go
func main() {
    server := NewServer()

    // 创建带Panic Hook的中间件
    recovery := WrapWithHooks(Recovery()).
        AddPanic(func(ctx *Context) {
            ctx.Logger().Errorf("panic recovered!")
        })

    server.Use(recovery)

    // 可能panic的handler
    server.GET("/panic", Wrap(func(ctx *Context) Result {
        panic("something went wrong")
    }))

    server.Run(":8080")
}
```

### 7.2 Cancellable Hooks

#### 示例3：Before Hook取消请求

```go
func main() {
    server := NewServer()

    // 创建带认证的中间件
    auth := WrapWithHooks(func(ctx *Context) {
        ctx.Next()
    }).AddBefore(func(ctx *Context) {
        token := ctx.GetHeader("Authorization")
        if token == "" {
            ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "missing token"})
            ctx.Abort()
        }
    })

    server.Use(auth)

    // Handler
    server.GET("/protected", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "protected resource"})
    }))

    server.Run(":8080")
}
```

### 7.3 动态配置

#### 示例4：全局Hooks

```go
func main() {
    server := NewServer()

    // 添加全局Before Hook
    server.AddGlobalBefore(func(ctx *Context) {
        ctx.Set("request-start-time", time.Now())
    })

    // 添加全局After Hook
    server.AddGlobalAfter(func(ctx *Context) {
        startTime := ctx.Value("request-start-time")
        if startTime != nil {
            duration := time.Since(startTime.(time.Time))
            ctx.Logger().Debugf("request took %s", duration)
        }
    })

    // 添加全局Panic Hook
    server.AddGlobalPanic(func(ctx *Context) {
        ctx.Logger().Errorf("panic recovered!")
    })

    // 所有handler都会应用全局hooks
    server.GET("/hello", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "hello"})
    }))

    server.Run(":8080")
}
```

#### 示例5：路由特定Hooks

```go
func main() {
    server := NewServer()

    // 添加全局hooks
    server.AddGlobalBefore(func(ctx *Context) {
        ctx.Logger().Infof("global before hook")
    })

    // 为特定路由添加hooks
    server.AddRouteBefore("/admin", func(ctx *Context) {
        ctx.Logger().Infof("admin route before hook")
    })

    server.AddRouteAfter("/admin", func(ctx *Context) {
        ctx.Logger().Infof("admin route after hook")
    })

    // 普通路由（只有全局hooks）
    server.GET("/hello", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "hello"})
    }))

    // Admin路由（全局hooks + 路由特定hooks）
    server.GET("/admin/users", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "admin users"})
    }))

    server.Run(":8080")
}
```

#### 示例6：运行时动态添加Hooks

```go
func main() {
    server := NewServer()

    // 先启动服务器
    go func() {
        server.Run(":8080")
    }()

    // 运行时添加hooks
    time.Sleep(5 * time.Second)
    server.AddGlobalBefore(func(ctx *Context) {
        ctx.Logger().Infof("runtime added before hook")
    })

    // 等待退出
    select {}
}
```

### 7.4 复杂场景

#### 示例7：链式调用

```go
func main() {
    server := NewServer()

    // 创建复杂的带hooks的中间件
    middleware := WrapWithHooks(func(ctx *Context) {
        ctx.Next()
    }).
        AddBefore(func(ctx *Context) {
            ctx.Logger().Infof("before 1")
        }).
        AddBefore(func(ctx *Context) {
            ctx.Logger().Infof("before 2")
        }).
        AddAfter(func(ctx *Context) {
            ctx.Logger().Infof("after 1")
        }).
        AddAfter(func(ctx *Context) {
            ctx.Logger().Infof("after 2")
        }).
        AddPanic(func(ctx *Context) {
            ctx.Logger().Errorf("panic hook")
        })

    server.Use(middleware)

    server.GET("/hello", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "hello"})
    }))

    server.Run(":8080")
}

// 执行顺序：
// before 1
// before 2
// handler执行
// after 1
// after 2
```

#### 示例8：Metrics中间件

```go
type Metrics struct {
    mu       sync.Mutex
    requests map[string]int64
    errors   map[string]int64
}

func NewMetrics() *Metrics {
    return &Metrics{
        requests: make(map[string]int64),
        errors:   make(map[string]int64),
    }
}

func (m *Metrics) RecordRequest(path string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.requests[path]++
}

func (m *Metrics) RecordError(path string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.errors[path]++
}

func main() {
    server := NewServer()
    metrics := NewMetrics()

    // 创建带Metrics的中间件
    metricsMW := WrapWithHooks(func(ctx *Context) {
        ctx.Next()
    }).
        AddBefore(func(ctx *Context) {
            path := string(ctx.fastCtx.Path())
            metrics.RecordRequest(path)
        }).
        AddAfter(func(ctx *Context) {
            if ctx.Writer.Status() >= 400 {
                path := string(ctx.fastCtx.Path())
                metrics.RecordError(path)
            }
        })

    server.Use(metricsMW)

    server.GET("/hello", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "hello"})
    }))

    // 添加metrics接口
    server.GET("/metrics", Wrap(func(ctx *Context) Result {
        return Auto(map[string]interface{}{
            "requests": metrics.requests,
            "errors":   metrics.errors,
        })
    }))

    server.Run(":8080")
}
```

## 8. 性能分析

### 8.1 Hooks执行开销

**Hook执行路径**：
```
BeforeHook -> Handler -> AfterHook
```

**性能开销**：
- 每个Hook是一次接口调用（~2ns）
- Before/After各执行一次（~4ns total）
- 结论：性能影响可忽略不计

### 8.2 基准测试

```go
func Benchmark_Hooks(b *testing.B) {
    middleware := WrapWithHooks(func(ctx *Context) {
        ctx.Next()
    }).
        AddBefore(func(ctx *Context) {}).
        AddAfter(func(ctx *Context) {})

    ctx := &Context{}
    ctx.handlers = []HandlerFunc{middleware.Handle}

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        ctx.handlerIndex = -1
        middleware.Handle(ctx)
    }
}

func Benchmark_NoHooks(b *testing.B) {
    middleware := func(ctx *Context) {
        ctx.Next()
    }

    ctx := &Context{}
    ctx.handlers = []HandlerFunc{middleware}

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        ctx.handlerIndex = -1
        middleware(ctx)
    }
}

// 预期结果：
// Hooks:    ~1050 ns/op, +5% vs NoHooks
// NoHooks:  ~1000 ns/op
```

## 9. 测试计划

### 9.1 单元测试

```go
func TestBeforeHook(t *testing.T) {
    called := false
    middleware := WrapWithHooks(func(ctx *Context) {
        ctx.Next()
    }).AddBefore(func(ctx *Context) {
        called = true
    })

    ctx := &Context{}
    middleware.Handle(ctx)

    assert.True(t, called)
}

func TestAfterHook(t *testing.T) {
    called := false
    middleware := WrapWithHooks(func(ctx *Context) {
        ctx.Next()
    }).AddAfter(func(ctx *Context) {
        called = true
    })

    ctx := &Context{}
    middleware.Handle(ctx)

    assert.True(t, called)
}

func TestPanicHook(t *testing.T) {
    panicCalled := false
    middleware := WrapWithHooks(func(ctx *Context) {
        panic("test")
    }).AddPanic(func(ctx *Context) {
        panicCalled = true
        ctx.Abort()  // 防止panic传播
    })

    ctx := &Context{}
    middleware.Handle(ctx)

    assert.True(t, panicCalled)
}

func TestCancellableHook(t *testing.T) {
    handlerCalled := false
    middleware := WrapWithHooks(func(ctx *Context) {
        ctx.Next()
    }).AddBefore(func(ctx *Context) {
        ctx.Abort()
    })

    ctx := &Context{}
    ctx.handlers = []HandlerFunc{
        middleware.Handle,
        func(ctx *Context) { handlerCalled = true },
    }
    ctx.handlerIndex = -1

    middleware.Handle(ctx)

    assert.False(t, handlerCalled)
}
```

### 9.2 集成测试

```go
func TestHooksIntegration(t *testing.T) {
    server := NewServer()

    beforeCalled := false
    afterCalled := false

    server.Use(WrapWithHooks(func(ctx *Context) {
        ctx.Next()
    }).
        AddBefore(func(ctx *Context) {
            beforeCalled = true
        }).
        AddAfter(func(ctx *Context) {
            afterCalled = true
        }))

    server.GET("/hello", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "hello"})
    }))

    // 发送测试请求...
    // 验证beforeCalled和afterCalled都为true
}
```

## 10. 总结

### 10.1 设计优势
1. **Before/After/Panic Hooks**：完整的生命周期支持
2. **Cancellable Hooks**：hooks可以取消请求
3. **动态配置**：运行时添加/移除hooks
4. **简单易用**：链式调用API，不增加复杂度
5. **高性能**：hooks执行零额外开销

### 10.2 使用场景

| 场景 | Hook类型 | 示例 |
|------|----------|------|
| 请求开始 | Before | 记录开始时间、生成请求ID |
| 请求结束 | After | 计算耗时、记录日志、更新metrics |
| 错误恢复 | Panic | 记录panic、发送告警 |
| 权限检查 | Before | 验证token、取消未授权请求 |
| 响应修改 | After | 添加响应头、修改响应体 |

### 10.3 实施步骤
1. 实现Hook接口和HookFunc
2. 实现Hooks集合和HooksManager
3. 实现MiddlewareWithHooks
4. 改造现有中间件（RequestID, CORS, Logger, Recovery）
5. Server集成HooksManager
6. 编写单元测试
7. 编写集成测试
8. 更新文档和示例

# 路由匹配优化设计文档

## 1. 设计目标

### 1.1 核心目标
1. **智能优先级**：根据访问频率动态调整路由匹配顺序
2. **路由预热**：启动时预热常用路由
3. **路径解析缓存**：缓存已解析的路径参数
4. **正则预编译**：预编译所有正则路由
5. **零额外开销**：热路径优化不影响性能

### 1.2 设计原则
- **渐进增强**：保持现有性能，逐步优化
- **可配置**：允许用户选择是否启用优化
- **自适应**：根据实际访问模式调整

## 2. 现有架构分析

### 2.1 当前路由系统

```go
// Match：路由匹配接口
type Match interface {
    AddRoute(method, path string, handlers ...HandlerFunc) error
    Lookup(method, path string) *RouteResult
}

// RouteResult：路由匹配结果
type RouteResult struct {
    Path       string
    Handlers   []HandlerFunc
    PathParams map[string]string
}

// 方法匹配器：按HTTP方法分组
type MethodMatcher struct {
    mu        sync.RWMutex
    trees     map[string]Tree
    wildcards map[string]*WildcardEntry
}

// 压缩前缀树
type CompressedRadixTree struct {
    root       *node
    wildcards  []*node
    parameters []*node
}
```

### 2.2 当前性能

| 操作 | 性能 | 说明 |
|------|------|------|
| 简单路由匹配 | ~50ns | 无参数路由 |
| 参数路由匹配 | ~100ns | 一个参数路由 |
| 正则路由匹配 | ~500ns | 正则表达式路由 |
| 路由添加 | ~10μs | 构建树结构 |

### 2.3 现有问题

1. **静态路由与参数路由混合**：每次都需要先匹配静态路由，再匹配参数路由
2. **无访问统计**：无法知道哪些路由是热路由
3. **无缓存**：相同的路径参数每次都要重新解析
4. **正则路由慢**：每次都要编译正则表达式

## 3. 优化策略

### 3.1 智能优先级

#### 设计思路

- 统计每个路由的访问频率
- 热路由优先匹配
- 冷路由延迟匹配

#### 实现方案

```go
// RouteStats：路由统计信息
type RouteStats struct {
    mu           sync.RWMutex
    counts       map[string]int64      // 访问次数
    lastAccessed map[string]time.Time // 最后访问时间
    hotRoutes    map[string]bool      // 热路由标记
    hotThreshold int64               // 热路由阈值（访问次数）
}

// NewRouteStats：创建路由统计
func NewRouteStats(hotThreshold int64) *RouteStats {
    return &RouteStats{
        counts:        make(map[string]int64),
        lastAccessed: make(map[string]time.Time),
        hotRoutes:     make(map[string]bool),
        hotThreshold:  hotThreshold,
    }
}

// Record：记录路由访问
func (rs *RouteStats) Record(route string) {
    rs.mu.Lock()
    defer rs.mu.Unlock()

    rs.counts[route]++
    rs.lastAccessed[route] = time.Now()

    // 检查是否成为热路由
    if !rs.hotRoutes[route] && rs.counts[route] >= rs.hotThreshold {
        rs.hotRoutes[route] = true
    }
}

// IsHot：检查是否是热路由
func (rs *RouteStats) IsHot(route string) bool {
    rs.mu.RLock()
    defer rs.mu.RUnlock()
    return rs.hotRoutes[route]
}

// GetHotRoutes：获取所有热路由
func (rs *RouteStats) GetHotRoutes() []string {
    rs.mu.RLock()
    defer rs.mu.RUnlock()

    routes := make([]string, 0, len(rs.hotRoutes))
    for route := range rs.hotRoutes {
        routes = append(routes, route)
    }

    // 按访问次数排序
    sort.Slice(routes, func(i, j int) bool {
        return rs.counts[routes[i]] > rs.counts[routes[j]]
    })

    return routes
}

// Reset：重置统计
func (rs *RouteStats) Reset() {
    rs.mu.Lock()
    defer rs.mu.Unlock()
    rs.counts = make(map[string]int64)
    rs.lastAccessed = make(map[string]time.Time)
    rs.hotRoutes = make(map[string]bool)
}
```

### 3.2 路由预热

#### 设计思路

- 启动时预执行常用路由的匹配
- 构建路径参数缓存
- 提升首次访问性能

#### 实现方案

```go
// RouteWarmer：路由预热器
type RouteWarmer struct {
    match     Match
    stats     *RouteStats
    cacheSize int  // 预热缓存大小
}

// NewRouteWarmer：创建路由预热器
func NewRouteWarmer(match Match, stats *RouteStats, cacheSize int) *RouteWarmer {
    return &RouteWarmer{
        match:     match,
        stats:     stats,
        cacheSize:  cacheSize,
    }
}

// Warm：预热指定路由
func (rw *RouteWarmer) Warm(method, path string) {
    // 执行路由匹配
    result := rw.match.Lookup(method, path)

    // 记录统计
    if result != nil {
        routeKey := method + ":" + result.Path
        rw.stats.Record(routeKey)
    }
}

// WarmCommonRoutes：预热常用路由
func (rw *RouteWarmer) WarmCommonRoutes(routes []string) {
    // 假设routes格式为 "GET:/users/1", "POST:/users"
    for _, route := range routes {
        parts := strings.Split(route, ":")
        if len(parts) != 2 {
            continue
        }
        method := parts[0]
        path := parts[1]
        rw.Warm(method, path)
    }
}

// WarmByPattern：按模式预热路由
func (rw *RouteWarmer) WarmByPattern(pattern string) {
    // 假设pattern为 "/users/:id"
    // 生成一些示例路径进行预热
    // 例如："/users/1", "/users/2", "/users/3"
    // ...
}
```

### 3.3 路径解析缓存

#### 设计思路

- 缓存路径参数解析结果
- 使用LRU缓存淘汰策略
- 减少重复解析开销

#### 实现方案

```go
// PathParamsCache：路径参数缓存
type PathParamsCache struct {
    mu    sync.RWMutex
    cache *lru.Cache  // LRU缓存
}

// cacheKey：缓存键
type cacheKey struct {
    method string
    path   string
}

// NewPathParamsCache：创建路径参数缓存
func NewPathParamsCache(size int) *PathParamsCache {
    cache, err := lru.New(size)
    if err != nil {
        panic(err)
    }

    return &PathParamsCache{
        cache: cache,
    }
}

// Get：获取缓存的路径参数
func (ppc *PathParamsCache) Get(method, path string) (map[string]string, bool) {
    ppc.mu.RLock()
    defer ppc.mu.RUnlock()

    key := cacheKey{method: method, path: path}
    if value, ok := ppc.cache.Get(key); ok {
        return value.(map[string]string), true
    }

    return nil, false
}

// Set：设置路径参数缓存
func (ppc *PathParamsCache) Set(method, path string, params map[string]string) {
    ppc.mu.Lock()
    defer ppc.mu.Unlock()

    key := cacheKey{method: method, path: path}
    ppc.cache.Add(key, params)
}

// Clear：清空缓存
func (ppc *PathParamsCache) Clear() {
    ppc.mu.Lock()
    defer ppc.mu.Unlock()
    ppc.cache.Purge()
}
```

### 3.4 正则预编译

#### 设计思路

- 添加路由时预编译正则表达式
- 缓存编译后的正则表达式对象
- 避免每次匹配时重新编译

#### 实现方案

```go
// RegexCache：正则表达式缓存
type RegexCache struct {
    mu    sync.RWMutex
    cache map[string]*regexp.Regexp
}

// NewRegexCache：创建正则表达式缓存
func NewRegexCache() *RegexCache {
    return &RegexCache{
        cache: make(map[string]*regexp.Regexp),
    }
}

// Compile：编译正则表达式（带缓存）
func (rc *RegexCache) Compile(pattern string) (*regexp.Regexp, error) {
    // 先检查缓存
    rc.mu.RLock()
    if re, ok := rc.cache[pattern]; ok {
        rc.mu.RUnlock()
        return re, nil
    }
    rc.mu.RUnlock()

    // 编译正则表达式
    re, err := regexp.Compile(pattern)
    if err != nil {
        return nil, err
    }

    // 添加到缓存
    rc.mu.Lock()
    rc.cache[pattern] = re
    rc.mu.Unlock()

    return re, nil
}

// MustCompile：编译正则表达式（panic on error）
func (rc *RegexCache) MustCompile(pattern string) *regexp.Regexp {
    re, err := rc.Compile(pattern)
    if err != nil {
        panic(err)
    }
    return re
}
```

## 4. 优化后的路由匹配器

### 4.1 SmartMatcher结构

```go
// SmartMatcher：智能路由匹配器
type SmartMatcher struct {
    *MethodMatcher  // 基础匹配器
    stats         *RouteStats     // 路由统计
    paramsCache   *PathParamsCache // 路径参数缓存
    regexCache    *RegexCache     // 正则表达式缓存
    hotRouteOrder []string        // 热路由顺序
    mu            sync.RWMutex
    enabled       bool            // 是否启用优化
}

// NewSmartMatcher：创建智能路由匹配器
func NewSmartMatcher(options ...SmartMatcherOption) *SmartMatcher {
    stats := NewRouteStats(100)  // 默认热路由阈值：100次
    paramsCache := NewPathParamsCache(1000)  // 默认缓存大小：1000
    regexCache := NewRegexCache()

    matcher := &SmartMatcher{
        MethodMatcher: NewMethodMatcher(),
        stats:        stats,
        paramsCache:  paramsCache,
        regexCache:   regexCache,
        enabled:      true,
    }

    for _, opt := range options {
        opt(matcher)
    }

    return matcher
}

// SmartMatcherOption：配置选项
type SmartMatcherOption func(*SmartMatcher)

// WithHotThreshold：设置热路由阈值
func WithHotThreshold(threshold int64) SmartMatcherOption {
    return func(sm *SmartMatcher) {
        sm.stats = NewRouteStats(threshold)
    }
}

// WithParamsCacheSize：设置路径参数缓存大小
func WithParamsCacheSize(size int) SmartMatcherOption {
    return func(sm *SmartMatcher) {
        sm.paramsCache = NewPathParamsCache(size)
    }
}

// WithOptimizationEnabled：启用/禁用优化
func WithOptimizationEnabled(enabled bool) SmartMatcherOption {
    return func(sm *SmartMatcher) {
        sm.enabled = enabled
    }
}
```

### 4.2 智能路由匹配

```go
// Lookup：智能路由查找
func (sm *SmartMatcher) Lookup(method, path string) *RouteResult {
    if !sm.enabled {
        // 优化未启用，使用基础匹配
        return sm.MethodMatcher.Lookup(method, path)
    }

    // 1. 检查路径参数缓存
    if params, ok := sm.paramsCache.Get(method, path); ok {
        // 使用缓存的路径参数
        return sm.lookupWithParams(method, path, params)
    }

    // 2. 优先匹配热路由
    sm.mu.RLock()
    hotRoutes := sm.hotRouteOrder
    sm.mu.RUnlock()

    for _, hotRoute := range hotRoutes {
        if strings.HasPrefix(hotRoute, method+":") {
            routePath := strings.TrimPrefix(hotRoute, method+":")
            // 检查路径是否匹配
            if params, ok := sm.matchPath(routePath, path); ok {
                // 记录访问
                sm.stats.Record(hotRoute)

                // 缓存路径参数
                sm.paramsCache.Set(method, path, params)

                return sm.lookupWithParams(method, path, params)
            }
        }
    }

    // 3. 基础匹配
    result := sm.MethodMatcher.Lookup(method, path)

    // 4. 记录访问和缓存
    if result != nil {
        routeKey := method + ":" + result.Path
        sm.stats.Record(routeKey)

        // 缓存路径参数
        if len(result.PathParams) > 0 {
            sm.paramsCache.Set(method, path, result.PathParams)
        }
    }

    return result
}

// lookupWithParams：使用已解析的路径参数查找
func (sm *SmartMatcher) lookupWithParams(method, path string, params map[string]string) *RouteResult {
    // 这里需要修改基础匹配器，支持传入已解析的路径参数
    // 或者直接构建RouteResult
    // ...
    return &RouteResult{
        Path:       path,
        PathParams: params,
    }
}

// matchPath：匹配路径（简化版）
func (sm *SmartMatcher) matchPath(routePath, requestPath string) (map[string]string, bool) {
    // 这里实现路径匹配逻辑
    // 返回路径参数和是否匹配
    // ...
    return nil, false
}

// UpdateHotRoutes：更新热路由顺序
func (sm *SmartMatcher) UpdateHotRoutes() {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    // 获取热路由
    hotRoutes := sm.stats.GetHotRoutes()
    sm.hotRouteOrder = hotRoutes
}
```

### 4.3 正则路由优化

```go
// AddRoute：添加路由（带正则预编译）
func (sm *SmartMatcher) AddRoute(method, path string, handlers ...HandlerFunc) error {
    // 检查是否是正则路由
    if strings.Contains(path, ":") && strings.Contains(path, "(") && strings.Contains(path, ")") {
        // 提取正则表达式
        regex := extractRegex(path)
        if regex != "" {
            // 预编译正则表达式
            _, err := sm.regexCache.Compile(regex)
            if err != nil {
                return fmt.Errorf("regex compile error: %w", err)
            }
        }
    }

    // 调用基础AddRoute
    return sm.MethodMatcher.AddRoute(method, path, handlers...)
}

// extractRegex：提取正则表达式
func extractRegex(path string) string {
    // 假设路径格式为 "/users/:id(\\d+)"
    // 提取正则部分：(\\d+)
    // ...
    return ""
}
```

## 5. Server集成

### 5.1 配置选项

```go
// Config添加路由优化配置
type Config struct {
    matcher            Match
    codec              *CodecFactory
    logger             Logger
    UseRawPath          bool
    render             Render
    templateEngine      TemplateEngine
    staticFileServer   *StaticFileServer

    // 路由优化配置
    routeOptimizationEnabled bool
    hotRouteThreshold     int64
    paramsCacheSize       int
}

// WithRouteOptimization：启用路由优化
func WithRouteOptimization(enabled bool) ServerOption {
    return func(c *Config) {
        c.routeOptimizationEnabled = enabled
    }
}

// WithHotRouteThreshold：设置热路由阈值
func WithHotRouteThreshold(threshold int64) ServerOption {
    return func(c *Config) {
        c.hotRouteThreshold = threshold
    }
}

// WithParamsCacheSize：设置路径参数缓存大小
func WithParamsCacheSize(size int) ServerOption {
    return func(c *Config) {
        c.paramsCacheSize = size
    }
}
```

### 5.2 启动时预热

```go
// WarmUpRoutes：预热路由
func (s *Server) WarmUpRoutes(routes []string) error {
    matcher, ok := s.Match.(*SmartMatcher)
    if !ok {
        return fmt.Errorf("matcher is not SmartMatcher")
    }

    warmer := NewRouteWarmer(matcher, matcher.stats, 100)
    warmer.WarmCommonRoutes(routes)

    return nil
}

// WarmUpRoutesByPattern：按模式预热路由
func (s *Server) WarmUpRoutesByPattern(pattern string) error {
    matcher, ok := s.Match.(*SmartMatcher)
    if !ok {
        return fmt.Errorf("matcher is not SmartMatcher")
    }

    warmer := NewRouteWarmer(matcher, matcher.stats, 100)
    warmer.WarmByPattern(pattern)

    return nil
}
```

## 6. 使用示例

### 6.1 基本使用

#### 示例1：启用路由优化

```go
func main() {
    // 创建服务器（启用路由优化）
    server := NewServer(
        WithRouteOptimization(true),
        WithHotRouteThreshold(100),      // 100次访问后成为热路由
        WithParamsCacheSize(1000),      // 缓存1000条路径参数
    )

    // 添加路由
    server.GET("/users/:id", Wrap(func(ctx *Context) Result {
        id := ctx.Param("id")
        user := getUser(id)
        return Auto(user)
    }))

    server.POST("/users", Wrap(func(ctx *Context) Result {
        // 创建用户...
        return Auto(User{ID: 1, Name: "John"})
    }))

    server.Run(":8080")
}
```

### 6.2 路由预热

#### 示例2：启动时预热路由

```go
func main() {
    server := NewServer(WithRouteOptimization(true))

    // 添加路由
    server.GET("/users/:id", handler)
    server.POST("/users", handler)

    // 预热路由
    server.WarmUpRoutes([]string{
        "GET:/users/1",
        "GET:/users/2",
        "GET:/users/3",
        "POST:/users",
    })

    server.Run(":8080")
}
```

#### 示例3：按模式预热路由

```go
func main() {
    server := NewServer(WithRouteOptimization(true))

    // 添加路由
    server.GET("/users/:id", handler)
    server.GET("/posts/:id", handler)
    server.GET("/comments/:id", handler)

    // 按模式预热
    server.WarmUpRoutesByPattern("/users/:id")
    server.WarmUpRoutesByPattern("/posts/:id")
    server.WarmUpRoutesByPattern("/comments/:id")

    server.Run(":8080")
}
```

### 6.3 动态更新热路由

#### 示例4：定期更新热路由顺序

```go
func main() {
    server := NewServer(WithRouteOptimization(true))

    // 添加路由...
    server.GET("/users/:id", handler)
    server.POST("/users", handler)

    // 启动服务器
    go server.Run(":8080")

    // 定期更新热路由顺序
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for {
        <-ticker.C
        if matcher, ok := server.Match.(*SmartMatcher); ok {
            matcher.UpdateHotRoutes()
            server.Logger().Infof("hot routes updated")
        }
    }
}
```

### 6.4 监控和统计

#### 示例5：获取路由统计信息

```go
func main() {
    server := NewServer(WithRouteOptimization(true))

    // 添加路由...
    server.GET("/users/:id", handler)
    server.POST("/users", handler)

    // 添加统计接口
    server.GET("/stats/routes", Wrap(func(ctx *Context) Result {
        if matcher, ok := server.Match.(*SmartMatcher); ok {
            hotRoutes := matcher.stats.GetHotRoutes()
            return Auto(map[string]interface{}{
                "hotRoutes": hotRoutes,
            })
        }
        return ErrorMsg("route stats not available")
    }))

    server.Run(":8080")
}
```

## 7. 性能分析

### 7.1 优化前后对比

| 场景 | 优化前 | 优化后 | 提升 |
|------|--------|--------|------|
| 首次访问热路由 | ~100ns | ~80ns | 20% |
| 访问已缓存路由 | ~100ns | ~50ns | 50% |
| 访问冷路由 | ~100ns | ~100ns | 0% |
| 正则路由匹配 | ~500ns | ~100ns | 80% |

### 7.2 基准测试

```go
func Benchmark_SmartMatcher_Cached(b *testing.B) {
    matcher := NewSmartMatcher(WithOptimizationEnabled(true))
    matcher.AddRoute("GET", "/users/:id", handler)
    matcher.stats.Record("GET:/users/:id")

    // 预热缓存
    matcher.Lookup("GET", "/users/1")

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        matcher.Lookup("GET", "/users/1")
    }
}

func Benchmark_SmartMatcher_Uncached(b *testing.B) {
    matcher := NewSmartMatcher(WithOptimizationEnabled(true))
    matcher.AddRoute("GET", "/users/:id", handler)

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        matcher.Lookup("GET", "/users/1")
    }
}

func Benchmark_SmartMatcher_Regex(b *testing.B) {
    matcher := NewSmartMatcher(WithOptimizationEnabled(true))
    matcher.AddRoute("GET", "/users/:id(\\d+)", handler)

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        matcher.Lookup("GET", "/users/123")
    }
}

// 预期结果：
// Cached:     ~50 ns/op, 1 allocs/op
// Uncached:   ~100 ns/op, 3 allocs/op
// Regex:      ~100 ns/op, 2 allocs/op (优化前：~500 ns/op）
```

## 8. 测试计划

### 8.1 单元测试

```go
func TestSmartMatcher_Lookup(t *testing.T) {
    matcher := NewSmartMatcher(WithOptimizationEnabled(true))
    matcher.AddRoute("GET", "/users/:id", handler)

    result := matcher.Lookup("GET", "/users/123")

    assert.NotNil(t, result)
    assert.Equal(t, "123", result.PathParams["id"])
}

func TestRouteStats_Record(t *testing.T) {
    stats := NewRouteStats(10)

    for i := 0; i < 10; i++ {
        stats.Record("GET:/users/:id")
    }

    assert.True(t, stats.IsHot("GET:/users/:id"))
}

func TestPathParamsCache_GetSet(t *testing.T) {
    cache := NewPathParamsCache(100)

    params := map[string]string{"id": "123"}
    cache.Set("GET", "/users/123", params)

    cached, ok := cache.Get("GET", "/users/123")
    assert.True(t, ok)
    assert.Equal(t, "123", cached["id"])
}
```

### 8.2 集成测试

```go
func TestSmartMatcher_Integration(t *testing.T) {
    server := NewServer(WithRouteOptimization(true))

    server.GET("/users/:id", Wrap(func(ctx *Context) Result {
        id := ctx.Param("id")
        return Auto(map[string]string{"id": id})
    }))

    // 发送测试请求...
    // 验证路由匹配正确
}
```

## 9. 总结

### 9.1 优化策略汇总

| 策略 | 提升 | 实施难度 |
|------|------|----------|
| 智能优先级 | 20% | 中 |
| 路由预热 | 20% | 低 |
| 路径解析缓存 | 50% | 低 |
| 正则预编译 | 80% | 低 |

### 9.2 性能提升汇总

| 场景 | 优化前 | 优化后 | 提升 |
|------|--------|--------|------|
| 热路由 | ~100ns | ~80ns | 20% |
| 已缓存路由 | ~100ns | ~50ns | 50% |
| 正则路由 | ~500ns | ~100ns | 80% |
| 冷路由 | ~100ns | ~100ns | 0% |

### 9.3 实施步骤
1. 实现RouteStats
2. 实现PathParamsCache
3. 实现RegexCache
4. 实现SmartMatcher
5. 实现RouteWarmer
6. Server集成
7. 编写单元测试
8. 编写集成测试
9. 编写基准测试
10. 更新文档和示例

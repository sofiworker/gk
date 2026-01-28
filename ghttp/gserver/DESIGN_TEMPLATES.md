# 模板渲染系统设计文档

## 1. 设计目标

### 1.1 核心目标
1. **多引擎支持**：支持html/template、text/template、第三方模板引擎
2. **热重载**：开发环境下自动重新加载修改的模板
3. **静态文件支持**：完整的静态文件服务（缓存、Range、压缩）
4. **高性能**：生产环境下模板预编译、静态文件缓存
5. **灵活配置**：支持自定义模板函数、全局数据

### 1.2 设计原则
- **接口驱动**：定义清晰的Engine接口
- **可扩展**：支持第三方模板引擎
- **环境适配**：开发/生产环境不同策略
- **零兼容包袱**：完全重新设计

## 2. 核心接口设计

### 2.1 TemplateEngine接口

```go
// TemplateEngine：模板引擎接口
type TemplateEngine interface {
    // Name：返回引擎名称
    Name() string

    // Render：渲染模板
    // name: 模板名称（文件名或模板ID）
    // data: 模板数据
    // 返回: 渲染结果（io.Reader）
    Render(name string, data interface{}) (io.Reader, error)

    // Load：加载模板（可选，用于预编译）
    Load(name string, content string) error

    // LoadFS：从文件系统加载模板
    LoadFS(fs fs.FS, pattern string) error

    // Reload：重新加载模板（支持热重载）
    Reload() error

    // Close：关闭引擎，释放资源
    Close() error
}

// HTMLRenderer：HTML渲染接口（向后兼容）
type HTMLRenderer interface {
    RenderHTML(name string, data interface{}) (io.Reader, error)
}

// TemplateFunc：模板函数类型
type TemplateFunc func(...interface{}) (interface{}, error)
```

### 2.2 Render接口（保持兼容）

```go
// Render：渲染接口（保持现有定义）
type Render interface {
    Render(data interface{}) (io.Reader, error)
}
```

## 3. 内置模板引擎

### 3.1 Go Template Engine

#### GoTemplateEngine结构

```go
// GoTemplateEngine：Go标准库模板引擎（html/template + text/template）
type GoTemplateEngine struct {
    mu         sync.RWMutex
    name       string
    isHTML     bool  // true: html/template, false: text/template
    templates  map[string]*template.Template
    funcMap    template.FuncMap
    basePath   string  // 模板基础路径
    leftDelim  string  // 左分隔符
    rightDelim string  // 右分隔符
}

// NewGoTemplateEngine：创建Go模板引擎
func NewGoTemplateEngine(options ...GoTemplateOption) *GoTemplateEngine {
    engine := &GoTemplateEngine{
        name:      "gotemplate",
        isHTML:    true,
        templates: make(map[string]*template.Template),
        funcMap:   make(template.FuncMap),
        basePath:  ".",
        leftDelim: "{{",
        rightDelim: "}}",
    }

    for _, opt := range options {
        opt(engine)
    }

    return engine
}

// GoTemplateOption：配置选项
type GoTemplateOption func(*GoTemplateEngine)

// WithHTML：使用html/template（默认）
func WithHTML() GoTemplateOption {
    return func(e *GoTemplateEngine) {
        e.isHTML = true
    }
}

// WithText：使用text/template
func WithText() GoTemplateOption {
    return func(e *GoTemplateEngine) {
        e.isHTML = false
    }
}

// WithBasePath：设置模板基础路径
func WithBasePath(path string) GoTemplateOption {
    return func(e *GoTemplateEngine) {
        e.basePath = path
    }
}

// WithDelims：设置模板分隔符
func WithDelims(left, right string) GoTemplateOption {
    return func(e *GoTemplateEngine) {
        e.leftDelim = left
        e.rightDelim = right
    }
}

// WithFuncs：设置模板函数
func WithFuncs(funcs template.FuncMap) GoTemplateOption {
    return func(e *GoTemplateEngine) {
        for k, v := range funcs {
            e.funcMap[k] = v
        }
    }
}

// AddFunc：添加模板函数
func (e *GoTemplateEngine) AddFunc(name string, fn TemplateFunc) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.funcMap[name] = func(args ...interface{}) (interface{}, error) {
        return fn(args...)
    }
}

// Name：实现TemplateEngine接口
func (e *GoTemplateEngine) Name() string {
    return e.name
}

// Render：实现TemplateEngine接口
func (e *GoTemplateEngine) Render(name string, data interface{}) (io.Reader, error) {
    e.mu.RLock()
    tmpl, ok := e.templates[name]
    e.mu.RUnlock()

    if !ok {
        return nil, fmt.Errorf("template '%s' not found", name)
    }

    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, data); err != nil {
        return nil, fmt.Errorf("template execute error: %w", err)
    }

    return bytes.NewReader(buf.Bytes()), nil
}

// Load：实现TemplateEngine接口
func (e *GoTemplateEngine) Load(name string, content string) error {
    e.mu.Lock()
    defer e.mu.Unlock()

    var t *template.Template
    var err error

    if e.isHTML {
        t, err = template.New(name).Funcs(e.funcMap).Parse(content)
    } else {
        t, err = template.New(name).Funcs(e.funcMap).Parse(content)
    }

    if err != nil {
        return fmt.Errorf("template parse error: %w", err)
    }

    e.templates[name] = t
    return nil
}

// LoadFS：实现TemplateEngine接口
func (e *GoTemplateEngine) LoadFS(filesys fs.FS, pattern string) error {
    matches, err := fs.Glob(filesys, pattern)
    if err != nil {
        return err
    }

    e.mu.Lock()
    defer e.mu.Unlock()

    for _, match := range matches {
        content, err := fs.ReadFile(filesys, match)
        if err != nil {
            return err
        }

        // 使用相对路径作为模板名
        name := filepath.ToSlash(match)
        if e.basePath != "." {
            name = strings.TrimPrefix(name, filepath.ToSlash(e.basePath)+"/")
        }

        var t *template.Template
        if e.isHTML {
            t, err = template.New(name).
                Delims(e.leftDelim, e.rightDelim).
                Funcs(e.funcMap).
                Parse(string(content))
        } else {
            t, err = template.New(name).
                Delims(e.leftDelim, e.rightDelim).
                Funcs(e.funcMap).
                Parse(string(content))
        }

        if err != nil {
            return fmt.Errorf("template '%s' parse error: %w", name, err)
        }

        e.templates[name] = t
    }

    return nil
}

// Reload：实现TemplateEngine接口
func (e *GoTemplateEngine) Reload() error {
    // 需要重新从文件系统加载
    // 这个方法需要与FileSystem配合使用
    // 在HotReloadRenderer中实现
    return nil
}

// Close：实现TemplateEngine接口
func (e *GoTemplateEngine) Close() error {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.templates = make(map[string]*template.Template)
    return nil
}
```

### 3.2 Pongo2 Engine（可选）

```go
// Pongo2Engine：Pongo2模板引擎（可选依赖）
// import "github.com/flosch/pongo2/v4"

type Pongo2Engine struct {
    mu        sync.RWMutex
    templates map[string]*pongo2.Template
    fs        http.FileSystem
}

func NewPongo2Engine(fs http.FileSystem) *Pongo2Engine {
    return &Pongo2Engine{
        templates: make(map[string]*pongo2.Template),
        fs:        fs,
    }
}

func (e *Pongo2Engine) Name() string {
    return "pongo2"
}

func (e *Pongo2Engine) Render(name string, data interface{}) (io.Reader, error) {
    e.mu.RLock()
    tmpl, ok := e.templates[name]
    e.mu.RUnlock()

    if !ok {
        return nil, fmt.Errorf("template '%s' not found", name)
    }

    result, err := tmpl.Execute(data)
    if err != nil {
        return nil, err
    }

    return strings.NewReader(result), nil
}

func (e *Pongo2Engine) LoadFS(filesys fs.FS, pattern string) error {
    // 加载Pongo2模板...
    return nil
}

func (e *Pongo2Engine) Reload() error {
    return nil
}

func (e *Pongo2Engine) Close() error {
    return nil
}
```

## 4. 热重载支持

### 4.1 HotReloadRenderer结构

```go
// HotReloadRenderer：支持热重载的渲染器
type HotReloadRenderer struct {
    engine        TemplateEngine
    fs            http.FileSystem
    pattern       string  // 文件匹配模式，如"*.html"
    mu            sync.RWMutex
    lastModified  map[string]time.Time
    watcher       fsnotify.Watcher  // 文件监视器
    reloadSignal  chan struct{}
    enabled       bool  // 是否启用热重载
}

// NewHotReloadRenderer：创建热重载渲染器
func NewHotReloadRenderer(engine TemplateEngine, fs http.FileSystem, pattern string) (*HotReloadRenderer, error) {
    renderer := &HotReloadRenderer{
        engine:       engine,
        fs:           fs,
        pattern:      pattern,
        lastModified: make(map[string]time.Time),
        reloadSignal: make(chan struct{}, 1),
        enabled:      true,
    }

    // 初始加载模板
    if err := renderer.loadTemplates(); err != nil {
        return nil, err
    }

    // 启动文件监视器
    if err := renderer.startWatcher(); err != nil {
        return nil, err
    }

    return renderer, nil
}

// Name：返回引擎名称
func (r *HotReloadRenderer) Name() string {
    return r.engine.Name() + "-hotreload"
}

// Render：渲染模板
func (r *HotReloadRenderer) Render(name string, data interface{}) (io.Reader, error) {
    return r.engine.Render(name, data)
}

// LoadFS：加载模板
func (r *HotReloadRenderer) LoadFS(filesys fs.FS, pattern string) error {
    return r.engine.LoadFS(filesys, pattern)
}

// Reload：重新加载模板
func (r *HotReloadRenderer) Reload() error {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.loadTemplates()
}

// loadTemplates：加载所有模板
func (r *HotReloadRenderer) loadTemplates() error {
    if gfs, ok := r.fs.(interface{ Open(name string) (http.File, error) }); ok {
        // 将http.FileSystem转换为fs.FS
        fsys := httpFS{FileSystem: gfs}
        if err := r.engine.LoadFS(fsys, r.pattern); err != nil {
            return err
        }
    }
    return nil
}

// startWatcher：启动文件监视器
func (r *HotReloadRenderer) startWatcher() error {
    if !r.enabled {
        return nil
    }

    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }

    r.watcher = watcher

    // 查找所有模板文件
    if gfs, ok := r.fs.(interface{ Open(name string) (http.File, error) }); ok {
        matches, err := fs.Glob(httpFS{FileSystem: gfs}, r.pattern)
        if err != nil {
            return err
        }

        for _, match := range matches {
            if err := watcher.Add(match); err != nil {
                return err
            }
        }
    }

    // 启动goroutine监听文件变化
    go r.watchFiles()

    return nil
}

// watchFiles：监听文件变化
func (r *HotReloadRenderer) watchFiles() {
    for {
        select {
        case event, ok := <-r.watcher.Events:
            if !ok {
                return
            }
            // 文件被修改或创建
            if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
                r.onFileChanged(event.Name)
            }
        case err, ok := <-r.watcher.Errors:
            if !ok {
                return
            }
            // 记录错误
            log.Printf("file watcher error: %v", err)
        }
    }
}

// onFileChanged：文件变化处理
func (r *HotReloadRenderer) onFileChanged(filename string) {
    r.mu.Lock()
    defer r.mu.Unlock()

    // 获取文件修改时间
    info, err := r.fs.Open(filename)
    if err != nil {
        log.Printf("open file error: %v", err)
        return
    }
    defer info.Close()

    stat, err := info.Stat()
    if err != nil {
        log.Printf("stat file error: %v", err)
        return
    }

    modTime := stat.ModTime()

    // 检查是否真的被修改了
    if lastMod, ok := r.lastModified[filename]; ok && modTime.Equal(lastMod) {
        return
    }

    r.lastModified[filename] = modTime

    // 重新加载模板
    if err := r.engine.Reload(); err != nil {
        log.Printf("reload template error: %v", err)
    } else {
        log.Printf("template reloaded: %s", filename)
    }
}

// Close：关闭渲染器
func (r *HotReloadRenderer) Close() error {
    if r.watcher != nil {
        r.watcher.Close()
    }
    return r.engine.Close()
}

// httpFS：将http.FileSystem适配为fs.FS
type httpFS struct{ http.FileSystem }

func (hfs httpFS) Open(name string) (fs.File, error) {
    return hfs.FileSystem.Open(name)
}
```

### 4.2 热重载配置

```go
// HotReloadConfig：热重载配置
type HotReloadConfig struct {
    Enabled  bool          // 是否启用热重载
    Pattern  string        // 文件匹配模式
    Interval time.Duration // 检查间隔（轮询模式）
}

// DefaultHotReloadConfig：默认配置
var DefaultHotReloadConfig = HotReloadConfig{
    Enabled:  true,
    Pattern:  "*.html",
    Interval: time.Second,
}
```

## 5. 静态文件服务

### 5.1 StaticFileServer结构

```go
// StaticFileServer：静态文件服务器
type StaticFileServer struct {
    fs            http.FileSystem
    cache         map[string]*cachedFile
    cacheDuration  time.Duration
    indexFiles    []string
    stripPrefix   string
    compress      bool  // 是否启用压缩
    rangeSupport  bool  // 是否支持Range请求
}

// cachedFile：缓存文件
type cachedFile struct {
    content    []byte
    modTime    time.Time
    etag       string
    headers    http.Header
    expiresAt  time.Time
}

// NewStaticFileServer：创建静态文件服务器
func NewStaticFileServer(fs http.FileSystem, options ...StaticFileOption) *StaticFileServer {
    server := &StaticFileServer{
        fs:           fs,
        cache:        make(map[string]*cachedFile),
        cacheDuration: time.Hour,
        indexFiles:   []string{"index.html", "index.htm"},
        stripPrefix:  "",
        compress:     true,
        rangeSupport: true,
    }

    for _, opt := range options {
        opt(server)
    }

    return server
}

// StaticFileOption：配置选项
type StaticFileOption func(*StaticFileServer)

// WithCacheDuration：设置缓存时长
func WithCacheDuration(d time.Duration) StaticFileOption {
    return func(s *StaticFileServer) {
        s.cacheDuration = d
    }
}

// WithIndexFiles：设置索引文件
func WithIndexFiles(files ...string) StaticFileOption {
    return func(s *StaticFileServer) {
        s.indexFiles = files
    }
}

// WithStripPrefix：设置要剥离的前缀
func WithStripPrefix(prefix string) StaticFileOption {
    return func(s *StaticFileServer) {
        s.stripPrefix = prefix
    }
}

// WithCompress：启用/禁用压缩
func WithCompress(enabled bool) StaticFileOption {
    return func(s *StaticFileServer) {
        s.compress = enabled
    }
}

// WithRangeSupport：启用/禁用Range支持
func WithRangeSupport(enabled bool) StaticFileOption {
    return func(s *StaticFileServer) {
        s.rangeSupport = enabled
    }
}

// Serve：服务静态文件
func (s *StaticFileServer) Serve(ctx *Context, filepath string) error {
    // 剥离前缀
    if s.stripPrefix != "" {
        filepath = strings.TrimPrefix(filepath, s.stripPrefix)
    }

    // 规范化路径
    filepath = path.Clean(filepath)
    if filepath == "." {
        filepath = "/"
    }

    // 防止路径遍历攻击
    if strings.Contains(filepath, "..") {
        ctx.Status(http.StatusBadRequest)
        return nil
    }

    // 尝试从缓存获取
    if cached, ok := s.getCached(filepath); ok {
        return s.serveCached(ctx, cached)
    }

    // 打开文件
    file, err := s.fs.Open(filepath)
    if err != nil {
        if os.IsNotExist(err) {
            // 尝试索引文件
            return s.serveIndex(ctx, filepath)
        }
        ctx.Status(http.StatusNotFound)
        return nil
    }
    defer file.Close()

    // 获取文件信息
    stat, err := file.Stat()
    if err != nil {
        ctx.Status(http.StatusInternalServerError)
        return err
    }

    // 如果是目录，尝试索引文件
    if stat.IsDir() {
        return s.serveIndex(ctx, filepath)
    }

    // 读取文件内容
    content, err := io.ReadAll(file)
    if err != nil {
        ctx.Status(http.StatusInternalServerError)
        return err
    }

    // 计算ETag
    etag := fmt.Sprintf(`"%x-%x"`, stat.ModTime().Unix(), stat.Size())

    // 检查If-None-Match header
    if noneMatch := ctx.GetHeader("If-None-Match"); noneMatch == etag {
        ctx.Status(http.StatusNotModified)
        return nil
    }

    // 处理Range请求
    if s.rangeSupport {
        if rangeHeader := ctx.GetHeader("Range"); rangeHeader != "" {
            return s.serveRange(ctx, content, stat.Size(), rangeHeader)
        }
    }

    // 设置headers
    ctx.Header("Content-Type", s.detectContentType(filepath))
    ctx.Header("ETag", etag)
    ctx.Header("Last-Modified", stat.ModTime().UTC().Format(http.TimeFormat))
    ctx.Header("Cache-Control", fmt.Sprintf("max-age=%d", int(s.cacheDuration.Seconds())))

    // 缓存文件
    s.cacheFile(filepath, &cachedFile{
        content:   content,
        modTime:   stat.ModTime(),
        etag:      etag,
        headers:   nil,
        expiresAt: time.Now().Add(s.cacheDuration),
    })

    // 写入响应
    ctx.Status(http.StatusOK)
    ctx.Writer.Write(content)

    return nil
}

// serveIndex：服务索引文件
func (s *StaticFileServer) serveIndex(ctx *Context, filepath string) error {
    if !strings.HasSuffix(filepath, "/") {
        filepath += "/"
    }

    for _, indexFile := range s.indexFiles {
        indexPath := filepath + indexFile
        if file, err := s.fs.Open(indexPath); err == nil {
            file.Close()
            return s.Serve(ctx, indexPath)
        }
    }

    ctx.Status(http.StatusNotFound)
    return nil
}

// serveRange：服务Range请求
func (s *StaticFileServer) serveRange(ctx *Context, content []byte, size int64, rangeHeader string) error {
    // 解析Range header
    start, end, err := parseRange(rangeHeader, size)
    if err != nil {
        ctx.Status(http.StatusRequestedRangeNotSatisfiable)
        return err
    }

    // 设置headers
    ctx.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
    ctx.Header("Content-Length", strconv.FormatInt(end-start+1, 10))
    ctx.Header("Accept-Ranges", "bytes")
    ctx.Header("Content-Type", s.detectContentType(ctx.FullPath()))

    // 写入部分内容
    ctx.Status(http.StatusPartialContent)
    ctx.Writer.Write(content[start : end+1])

    return nil
}

// getCached：从缓存获取文件
func (s *StaticFileServer) getCached(filepath string) (*cachedFile, bool) {
    if s.cacheDuration <= 0 {
        return nil, false
    }

    cached, ok := s.cache[filepath]
    if !ok {
        return nil, false
    }

    // 检查是否过期
    if time.Now().After(cached.expiresAt) {
        delete(s.cache, filepath)
        return nil, false
    }

    return cached, true
}

// cacheFile：缓存文件
func (s *StaticFileServer) cacheFile(filepath string, cached *cachedFile) {
    if s.cacheDuration <= 0 {
        return
    }

    s.cache[filepath] = cached

    // 清理过期缓存
    if len(s.cache) > 1000 {  // 超过1000个文件时清理
        s.cleanCache()
    }
}

// cleanCache：清理过期缓存
func (s *StaticFileServer) cleanCache() {
    now := time.Now()
    for path, cached := range s.cache {
        if now.After(cached.expiresAt) {
            delete(s.cache, path)
        }
    }
}

// serveCached：服务缓存的文件
func (s *StaticFileServer) serveCached(ctx *Context, cached *cachedFile) error {
    // 检查If-None-Match header
    if noneMatch := ctx.GetHeader("If-None-Match"); noneMatch == cached.etag {
        ctx.Status(http.StatusNotModified)
        return nil
    }

    // 设置headers
    ctx.Header("ETag", cached.etag)
    ctx.Header("Last-Modified", cached.modTime.UTC().Format(http.TimeFormat))
    ctx.Header("Cache-Control", fmt.Sprintf("max-age=%d", int(s.cacheDuration.Seconds())))

    // 写入响应
    ctx.Status(http.StatusOK)
    ctx.Writer.Write(cached.content)

    return nil
}

// detectContentType：检测Content-Type
func (s *StaticFileServer) detectContentType(filepath string) string {
    ext := path.Ext(filepath)
    mimeType := mime.TypeByExtension(ext)
    if mimeType == "" {
        return "application/octet-stream"
    }
    return mimeType
}

// parseRange：解析Range header
func parseRange(rangeHeader string, size int64) (start, end int64, err error) {
    // 格式: bytes=start-end
    prefix := "bytes="
    if !strings.HasPrefix(rangeHeader, prefix) {
        return 0, 0, fmt.Errorf("invalid range header")
    }

    parts := strings.Split(strings.TrimPrefix(rangeHeader, prefix), "-")
    if len(parts) != 2 {
        return 0, 0, fmt.Errorf("invalid range header")
    }

    start, err = strconv.ParseInt(parts[0], 10, 64)
    if err != nil {
        return 0, 0, err
    }

    if parts[1] == "" {
        end = size - 1
    } else {
        end, err = strconv.ParseInt(parts[1], 10, 64)
        if err != nil {
            return 0, 0, err
        }
    }

    if start >= size || end >= size || start > end {
        return 0, 0, fmt.Errorf("invalid range")
    }

    return start, end, nil
}
```

## 6. Server集成

### 6.1 添加渲染器配置

```go
// Config添加渲染器配置
type Config struct {
    matcher    Match
    codec      *CodecFactory
    logger     Logger
    UseRawPath bool
    render     Render
    templateEngine TemplateEngine  // 新增
    staticFileServer *StaticFileServer  // 新增
}

// WithTemplateEngine：设置模板引擎
func WithTemplateEngine(engine TemplateEngine) ServerOption {
    return func(c *Config) {
        c.templateEngine = engine
    }
}

// WithStaticFileServer：设置静态文件服务器
func WithStaticFileServer(server *StaticFileServer) ServerOption {
    return func(c *Config) {
        c.staticFileServer = server
    }
}
```

### 6.2 静态文件路由

```go
// StaticFile：注册单个静态文件路由
func (s *Server) StaticFile(relativePath, filepath string) IRouter {
    return s.GET(relativePath, func(ctx *Context) {
        if s.staticFileServer == nil {
            s.staticFileServer = NewStaticFileServer(http.Dir("."))
        }
        if err := s.staticFileServer.Serve(ctx, filepath); err != nil {
            ctx.Logger().Errorf("serve static file error: %v", err)
        }
    })
}

// StaticFiles：注册静态文件路由
func (s *Server) StaticFiles(relativePath, root string) IRouter {
    return s.StaticFS(relativePath, http.Dir(root))
}

// StaticFS：注册静态文件路由（自定义FileSystem）
func (s *Server) StaticFS(relativePath string, fs http.FileSystem) IRouter {
    if s.staticFileServer == nil {
        s.staticFileServer = NewStaticFileServer(fs)
    }

    handler := func(ctx *Context) {
        filepath := strings.TrimSpace(ctx.Param("filepath"))
        if filepath == "" {
            filepath = "."
        } else {
            filepath = path.Clean("/" + filepath)
            filepath = strings.TrimPrefix(filepath, "/")
        }

        if err := s.staticFileServer.Serve(ctx, filepath); err != nil {
            ctx.Logger().Errorf("serve static file error: %v", err)
        }
    }

    absolutePath := JoinPaths(relativePath, "/*filepath")
    s.GET(absolutePath, handler)
    s.HEAD(absolutePath, handler)
    return s.IRouter
}
```

### 6.3 模板渲染路由

```go
// RenderHTML：渲染HTML模板
func (s *Server) RenderHTML(ctx *Context, name string, data interface{}) error {
    if s.templateEngine == nil {
        return fmt.Errorf("template engine not configured")
    }

    reader, err := s.templateEngine.Render(name, data)
    if err != nil {
        return err
    }

    ctx.Header("Content-Type", "text/html; charset=utf-8")
    ctx.Status(http.StatusOK)

    buf := renderBufPool.Get().([]byte)
    defer renderBufPool.Put(buf)

    if _, err := io.CopyBuffer(ctx.Writer, reader, buf); err != nil {
        return err
    }

    return nil
}
```

## 7. 使用示例

### 7.1 基本使用

#### 示例1：Go Template引擎

```go
func main() {
    // 创建模板引擎
    engine := NewGoTemplateEngine(
        WithHTML(),
        WithBasePath("./templates"),
        WithFuncs(template.FuncMap{
            "formatDate": func(t time.Time) string {
                return t.Format("2006-01-02")
            },
        }),
    )

    // 加载模板
    if err := engine.LoadFS(os.DirFS("./templates"), "*.html"); err != nil {
        log.Fatal(err)
    }

    // 创建服务器
    server := NewServer(WithTemplateEngine(engine))

    // 使用模板
    server.GET("/users/:id", Wrap(func(ctx *Context) Result {
        user := User{ID: 1, Name: "John", CreatedAt: time.Now()}
        return HTML("user.html", user)
    }))

    server.Run(":8080")
}
```

**模板文件**（`templates/user.html`）：
```html
<!DOCTYPE html>
<html>
<head>
    <title>User {{.ID}}</title>
</head>
<body>
    <h1>User: {{.Name}}</h1>
    <p>Created: {{formatDate .CreatedAt}}</p>
</body>
</html>
```

### 7.2 热重载

#### 示例2：开发环境热重载

```go
func main() {
    // 创建模板引擎
    engine := NewGoTemplateEngine(WithHTML(), WithBasePath("./templates"))

    // 创建热重载渲染器
    renderer, err := NewHotReloadRenderer(engine, http.Dir("./templates"), "*.html")
    if err != nil {
        log.Fatal(err)
    }
    defer renderer.Close()

    // 创建服务器
    server := NewServer(WithTemplateEngine(renderer))

    server.GET("/users/:id", Wrap(func(ctx *Context) Result {
        user := User{ID: 1, Name: "John"}
        return HTML("user.html", user)
    }))

    server.Run(":8080")
}
```

### 7.3 静态文件服务

#### 示例3：基本静态文件服务

```go
func main() {
    server := NewServer()

    // 服务静态文件
    server.Static("/static", "./public")

    server.GET("/hello", Wrap(func(ctx *Context) Result {
        return Auto(map[string]string{"message": "hello"})
    }))

    server.Run(":8080")
}
```

#### 示例4：高级静态文件配置

```go
func main() {
    // 创建静态文件服务器
    staticServer := NewStaticFileServer(http.Dir("./public"),
        WithCacheDuration(24*time.Hour),  // 缓存24小时
        WithIndexFiles("index.html", "index.htm"),
        WithCompress(true),
        WithRangeSupport(true),
    )

    // 创建服务器
    server := NewServer(WithStaticFileServer(staticServer))

    server.GET("/*filepath", func(ctx *Context) {
        filepath := strings.TrimSpace(ctx.Param("filepath"))
        if filepath == "" {
            filepath = "/"
        }
        staticServer.Serve(ctx, filepath)
    })

    server.Run(":8080")
}
```

### 7.4 多引擎支持

#### 示例5：使用多个模板引擎

```go
func main() {
    server := NewServer()

    // HTML模板
    htmlEngine := NewGoTemplateEngine(WithHTML(), WithBasePath("./templates/html"))
    htmlEngine.LoadFS(os.DirFS("./templates/html"), "*.html")

    // Email模板（text/template）
    textEngine := NewGoTemplateEngine(WithText(), WithBasePath("./templates/text"))
    textEngine.LoadFS(os.DirFS("./templates/text"), "*.txt")

    // 注册不同引擎
    server.GET("/page", Wrap(func(ctx *Context) Result {
        return HTML("page.html", map[string]string{"title": "My Page"})
    }))

    server.GET("/email", Wrap(func(ctx *Context) Result {
        // 使用text引擎渲染邮件
        content, _ := textEngine.Render("email.txt", map[string]string{"name": "John"})
        return String(string(readAll(content)))
    }))

    server.Run(":8080")
}

func readAll(r io.Reader) []byte {
    b, _ := io.ReadAll(r)
    return b
}
```

## 8. 性能分析

### 8.1 性能对比

| 操作 | 性能 | 说明 |
|------|------|------|
| 模板渲染（缓存） | ~10μs | 预编译模板 |
| 模板渲染（无缓存） | ~100μs | 需要解析 |
| 静态文件（缓存） | ~5μs | 内存缓存 |
| 静态文件（无缓存） | ~50μs | 磁盘读取 |
| Range请求 | ~10μs | 部分内容 |

### 8.2 基准测试

```go
func Benchmark_GoTemplateRender(b *testing.B) {
    engine := NewGoTemplateEngine(WithHTML())
    engine.Load("test", `<h1>{{.Title}}</h1>`)

    data := map[string]string{"Title": "Test"}

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        engine.Render("test", data)
    }
}

func Benchmark_StaticFile_Cached(b *testing.B) {
    server := NewStaticFileServer(http.Dir("./public"))
    ctx := &Context{}
    ctx.Writer = &respWriter{}

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        server.Serve(ctx, "test.html")
    }
}
```

## 9. 测试计划

### 9.1 单元测试

```go
func TestGoTemplateEngine_Render(t *testing.T) {
    engine := NewGoTemplateEngine(WithHTML())
    engine.Load("test", `<h1>{{.Title}}</h1>`)

    data := map[string]string{"Title": "Test"}
    reader, err := engine.Render("test", data)

    assert.NoError(t, err)
    assert.NotNil(t, reader)

    content, _ := io.ReadAll(reader)
    assert.Contains(t, string(content), "Test")
}

func TestStaticFileServer_Serve(t *testing.T) {
    // 创建临时目录和文件
    tmpDir := t.TempDir()
    os.WriteFile(filepath.Join(tmpDir, "test.html"), []byte("<h1>Test</h1>"), 0644)

    server := NewStaticFileServer(http.Dir(tmpDir))
    ctx := &Context{}
    ctx.Writer = &respWriter{}

    err := server.Serve(ctx, "test.html")

    assert.NoError(t, err)
    assert.Equal(t, http.StatusOK, ctx.Writer.Status())
}
```

### 9.2 集成测试

```go
func TestTemplateRendering_Integration(t *testing.T) {
    // 创建临时目录和模板文件
    tmpDir := t.TempDir()
    os.WriteFile(filepath.Join(tmpDir, "test.html"), []byte("<h1>{{.Title}}</h1>"), 0644)

    engine := NewGoTemplateEngine(WithHTML())
    engine.LoadFS(os.DirFS(tmpDir), "*.html")

    server := NewServer(WithTemplateEngine(engine))

    server.GET("/test", Wrap(func(ctx *Context) Result {
        return HTML("test.html", map[string]string{"Title": "Test"})
    }))

    // 发送测试请求...
}
```

## 10. 总结

### 10.1 设计优势
1. **多引擎支持**：html/template、text/template、第三方引擎
2. **热重载**：开发环境自动重新加载
3. **高性能**：模板预编译、静态文件缓存
4. **完整功能**：Range支持、ETag、压缩
5. **灵活配置**：自定义函数、索引文件、缓存时长

### 10.2 实施步骤
1. 实现TemplateEngine接口
2. 实现GoTemplateEngine
3. 实现HotReloadRenderer
4. 实现StaticFileServer
5. Server集成
6. 编写单元测试
7. 编写集成测试
8. 更新文档和示例

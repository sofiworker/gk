# Binding系统设计文档

## 1. 设计目标

### 1.1 核心目标
1. **字段级验证**：支持结构体标签驱动的验证规则
2. **零反射优化**：关键路径避免reflect，使用代码生成
3. **自动Content-Type选择**：根据Content-Type header自动选择绑定方式
4. **简洁易用**：类似Spring Boot的`@Valid`注解风格
5. **高性能**：支持零分配绑定路径

### 1.2 非目标
- 不提供复杂的验证规则组合（保持简单）
- 不支持动态验证规则（编译时确定）
- 不向后兼容现有`BindJSON`/`BindXML`（完全重新设计）

## 2. Binder接口设计

### 2.1 核心接口

```go
// Binder：请求绑定接口
// 负责将请求body绑定到目标对象
type Binder interface {
    Bind(ctx *Context, obj interface{}) error
}

// Validator：验证器接口
// 对象可以实现此接口提供自定义验证逻辑
type Validator interface {
    Validate() error
}

// ValidationErrors：验证错误集合
type ValidationErrors map[string]error

// Error：实现error接口
func (ve ValidationErrors) Error() string {
    var sb strings.Builder
    for field, err := range ve {
        sb.WriteString(field)
        sb.WriteString(": ")
        sb.WriteString(err.Error())
        sb.WriteString("; ")
    }
    return sb.String()
}

// Has：检查指定字段是否有错误
func (ve ValidationErrors) Has(field string) bool {
    _, ok := ve[field]
    return ok
}

// Get：获取指定字段的错误
func (ve ValidationErrors) Get(field string) error {
    return ve[field]
}

// Add：添加字段错误
func (ve ValidationErrors) Add(field string, err error) {
    ve[field] = err
}
```

### 2.2 Binder配置

```go
// BindingConfig：绑定配置
type BindingConfig struct {
    TagName      string // 结构体标签名称，默认"bind"
    StrictMode   bool   // 严格模式，未知字段返回错误
    MaxBodySize  int64  // 最大body大小，默认1MB
    SkipEmpty    bool   // 跳过空值验证
    DisableAuto  bool   // 禁用自动marshal（仅用于Content-Type检测）
}

// DefaultBindingConfig：默认绑定配置
var DefaultBindingConfig = BindingConfig{
    TagName:     "bind",
    StrictMode:  false,
    MaxBodySize: 1 << 20, // 1MB
    SkipEmpty:   false,
    DisableAuto: false,
}
```

## 3. 内置Binder实现

### 3.1 JSON Binder

```go
// JSONBinder：JSON绑定器
type JSONBinder struct {
    config BindingConfig
    codec  json.Marshaler // 可选的自定义JSON编解码器
}

// NewJSONBinder：创建JSON绑定器
func NewJSONBinder(cfg BindingConfig) *JSONBinder {
    if cfg.TagName == "" {
        cfg.TagName = DefaultBindingConfig.TagName
    }
    if cfg.MaxBodySize == 0 {
        cfg.MaxBodySize = DefaultBindingConfig.MaxBodySize
    }
    return &JSONBinder{config: cfg}
}

// Bind：实现Binder接口
func (b *JSONBinder) Bind(ctx *Context, obj interface{}) error {
    body := ctx.BodyBytes()

    // 检查body大小
    if int64(len(body)) > b.config.MaxBodySize {
        return fmt.Errorf("body too large: %d bytes", len(body))
    }

    // JSON解码
    if err := json.Unmarshal(body, obj); err != nil {
        return fmt.Errorf("json unmarshal error: %w", err)
    }

    // 验证
    if !b.config.DisableAuto {
        if err := validateStruct(obj, b.config); err != nil {
            return fmt.Errorf("validation error: %w", err)
        }
    }

    return nil
}
```

### 3.2 XML Binder

```go
// XMLBinder：XML绑定器
type XMLBinder struct {
    config BindingConfig
}

// NewXMLBinder：创建XML绑定器
func NewXMLBinder(cfg BindingConfig) *XMLBinder {
    if cfg.TagName == "" {
        cfg.TagName = DefaultBindingConfig.TagName
    }
    if cfg.MaxBodySize == 0 {
        cfg.MaxBodySize = DefaultBindingConfig.MaxBodySize
    }
    return &XMLBinder{config: cfg}
}

// Bind：实现Binder接口
func (b *XMLBinder) Bind(ctx *Context, obj interface{}) error {
    body := ctx.BodyBytes()

    // 检查body大小
    if int64(len(body)) > b.config.MaxBodySize {
        return fmt.Errorf("body too large: %d bytes", len(body))
    }

    // XML解码
    if err := xml.Unmarshal(body, obj); err != nil {
        return fmt.Errorf("xml unmarshal error: %w", err)
    }

    // 验证
    if !b.config.DisableAuto {
        if err := validateStruct(obj, b.config); err != nil {
            return fmt.Errorf("validation error: %w", err)
        }
    }

    return nil
}
```

### 3.3 Form Binder

```go
// FormBinder：Form绑定器（application/x-www-form-urlencoded）
type FormBinder struct {
    config BindingConfig
}

// NewFormBinder：创建Form绑定器
func NewFormBinder(cfg BindingConfig) *FormBinder {
    if cfg.TagName == "" {
        cfg.TagName = DefaultBindingConfig.TagName
    }
    return &FormBinder{config: cfg}
}

// Bind：实现Binder接口
func (b *JSONBinder) Bind(ctx *Context, obj interface{}) error {
    // 获取form数据
    postArgs := ctx.fastCtx.PostArgs()

    // 使用反射获取结构体字段
    v := reflect.ValueOf(obj)
    if v.Kind() != reflect.Ptr || v.IsNil() {
        return fmt.Errorf("obj must be a non-nil pointer")
    }
    v = v.Elem()

    if v.Kind() != reflect.Struct {
        return fmt.Errorf("obj must point to a struct")
    }

    t := v.Type()
    errors := make(ValidationErrors)

    // 遍历结构体字段
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        fieldV := v.Field(i)

        // 获取字段名（使用tag或字段名）
        name := field.Name
        if tag := field.Tag.Get(b.config.TagName); tag != "" {
            // 解析tag: "name=fieldName;required;max=100"
            parts := parseTag(tag)
            if n, ok := parts["name"]; ok {
                name = n
            }
        }

        // 获取form值
        value := postArgs.Peek(name)
        if len(value) == 0 {
            if b.config.SkipEmpty {
                continue
            }
            // 检查required
            if tag := field.Tag.Get(b.config.TagName); tag != "" {
                parts := parseTag(tag)
                if _, ok := parts["required"]; ok {
                    errors.Add(field.Name, fmt.Errorf("field '%s' is required", name))
                }
            }
            continue
        }

        // 设置字段值
        if err := setFieldValue(fieldV, value); err != nil {
            errors.Add(field.Name, err)
            continue
        }

        // 验证字段
        if tag := field.Tag.Get(b.config.TagName); tag != "" {
            parts := parseTag(tag)
            for rule, arg := range parts {
                if rule == "name" {
                    continue
                }
                if err := validateField(field, rule, arg, value); err != nil {
                    errors.Add(field.Name, err)
                }
            }
        }
    }

    if len(errors) > 0 {
        return errors
    }

    return nil
}

// setFieldValue：设置字段值
func setFieldValue(field reflect.Value, value []byte) error {
    switch field.Kind() {
    case reflect.String:
        field.SetString(string(value))
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
        if field.Kind() == reflect.Int64 && field.Type() == reflect.TypeOf(time.Duration(0)) {
            // time.Duration特殊处理
            d, err := time.ParseDuration(string(value))
            if err != nil {
                return err
            }
            field.SetInt(int64(d))
        } else {
            i, err := strconv.ParseInt(string(value), 10, 64)
            if err != nil {
                return err
            }
            field.SetInt(i)
        }
    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        i, err := strconv.ParseUint(string(value), 10, 64)
        if err != nil {
            return err
        }
        field.SetUint(i)
    case reflect.Float32, reflect.Float64:
        f, err := strconv.ParseFloat(string(value), 64)
        if err != nil {
            return err
        }
        field.SetFloat(f)
    case reflect.Bool:
        b, err := strconv.ParseBool(string(value))
        if err != nil {
            return err
        }
        field.SetBool(b)
    default:
        return fmt.Errorf("unsupported field type: %s", field.Kind())
    }
    return nil
}
```

### 3.4 Multipart Binder

```go
// MultipartBinder：Multipart绑定器（multipart/form-data）
type MultipartBinder struct {
    config BindingConfig
}

// NewMultipartBinder：创建Multipart绑定器
func NewMultipartBinder(cfg BindingConfig) *MultipartBinder {
    if cfg.TagName == "" {
        cfg.TagName = DefaultBindingConfig.TagName
    }
    return &MultipartBinder{config: cfg}
}

// Bind：实现Binder接口
func (b *MultipartBinder) Bind(ctx *Context, obj interface{}) error {
    // 使用反射获取结构体字段
    v := reflect.ValueOf(obj)
    if v.Kind() != reflect.Ptr || v.IsNil() {
        return fmt.Errorf("obj must be a non-nil pointer")
    }
    v = v.Elem()

    if v.Kind() != reflect.Struct {
        return fmt.Errorf("obj must point to a struct")
    }

    t := v.Type()
    errors := make(ValidationErrors)

    // 遍历结构体字段
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        fieldV := v.Field(i)

        // 检查是否是文件字段
        if field.Type.String() == "*multipart.FileHeader" {
            // 文件字段
            name := field.Name
            if tag := field.Tag.Get(b.config.TagName); tag != "" {
                parts := parseTag(tag)
                if n, ok := parts["name"]; ok {
                    name = n
                }
            }

            file, err := ctx.FormFile(name)
            if err != nil {
                if b.config.SkipEmpty || errors.Is(err, http.ErrMissingFile) {
                    continue
                }
                errors.Add(field.Name, err)
                continue
            }

            fieldV.Set(reflect.ValueOf(file))
            continue
        }

        // 普通字段（与FormBinder相同）
        name := field.Name
        if tag := field.Tag.Get(b.config.TagName); tag != "" {
            parts := parseTag(tag)
            if n, ok := parts["name"]; ok {
                name = n
            }
        }

        value := ctx.PostForm(name)
        if value == "" {
            if b.config.SkipEmpty {
                continue
            }
            // 检查required
            if tag := field.Tag.Get(b.config.TagName); tag != "" {
                parts := parseTag(tag)
                if _, ok := parts["required"]; ok {
                    errors.Add(field.Name, fmt.Errorf("field '%s' is required", name))
                }
            }
            continue
        }

        if err := setFieldValue(fieldV, []byte(value)); err != nil {
            errors.Add(field.Name, err)
            continue
        }

        // 验证字段
        if tag := field.Tag.Get(b.config.TagName); tag != "" {
            parts := parseTag(tag)
            for rule, arg := range parts {
                if rule == "name" {
                    continue
                }
                if err := validateField(field, rule, arg, []byte(value)); err != nil {
                    errors.Add(field.Name, err)
                }
            }
        }
    }

    if len(errors) > 0 {
        return errors
    }

    return nil
}
```

## 4. 验证规则设计

### 4.1 内置验证规则

```go
// validateField：验证单个字段
func validateField(field reflect.StructField, rule, arg string, value []byte) error {
    valueStr := string(value)

    switch rule {
    case "required":
        if valueStr == "" {
            return fmt.Errorf("field is required")
        }

    case "min":
        min, err := strconv.Atoi(arg)
        if err != nil {
            return fmt.Errorf("invalid min value: %s", arg)
        }
        switch field.Kind() {
        case reflect.String:
            if len(valueStr) < min {
                return fmt.Errorf("value length must be >= %d", min)
            }
        case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
            i, _ := strconv.ParseInt(valueStr, 10, 64)
            if i < int64(min) {
                return fmt.Errorf("value must be >= %d", min)
            }
        }

    case "max":
        max, err := strconv.Atoi(arg)
        if err != nil {
            return fmt.Errorf("invalid max value: %s", arg)
        }
        switch field.Kind() {
        case reflect.String:
            if len(valueStr) > max {
                return fmt.Errorf("value length must be <= %d", max)
            }
        case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
            i, _ := strconv.ParseInt(valueStr, 10, 64)
            if i > int64(max) {
                return fmt.Errorf("value must be <= %d", max)
            }
        }

    case "len":
        length, err := strconv.Atoi(arg)
        if err != nil {
            return fmt.Errorf("invalid length: %s", arg)
        }
        if len(valueStr) != length {
            return fmt.Errorf("value length must be %d", length)
        }

    case "email":
        if !isValidEmail(valueStr) {
            return fmt.Errorf("invalid email format")
        }

    case "url":
        if !isValidURL(valueStr) {
            return fmt.Errorf("invalid URL format")
        }

    case "pattern":
        matched, err := regexp.MatchString(arg, valueStr)
        if err != nil {
            return fmt.Errorf("invalid pattern: %s", arg)
        }
        if !matched {
            return fmt.Errorf("value does not match pattern '%s'", arg)
        }

    case "oneof":
        allowed := strings.Split(arg, ",")
        found := false
        for _, a := range allowed {
            if valueStr == strings.TrimSpace(a) {
                found = true
                break
            }
        }
        if !found {
            return fmt.Errorf("value must be one of: %s", arg)
        }

    default:
        return fmt.Errorf("unknown validation rule: %s", rule)
    }

    return nil
}

// 辅助函数
func isValidEmail(email string) bool {
    // 简化的email验证
    emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
    return emailRegex.MatchString(email)
}

func isValidURL(url string) bool {
    // 简化的URL验证
    _, err := neturl.ParseRequestURI(url)
    return err == nil
}
```

### 4.2 parseTag：解析结构体标签

```go
// parseTag：解析结构体标签
// tag格式: "name=fieldName;required;max=100"
// 返回: map[string]string{"name": "fieldName", "required": "", "max": "100"}
func parseTag(tag string) map[string]string {
    result := make(map[string]string)
    parts := strings.Split(tag, ";")

    for _, part := range parts {
        part = strings.TrimSpace(part)
        if part == "" {
            continue
        }

        // 检查是否包含等号
        if idx := strings.Index(part, "="); idx > 0 {
            key := strings.TrimSpace(part[:idx])
            value := strings.TrimSpace(part[idx+1:])
            result[key] = value
        } else {
            // 没有等号，表示是布尔标志
            result[part] = ""
        }
    }

    return result
}
```

### 4.3 validateStruct：验证整个结构体

```go
// validateStruct：验证结构体
// 使用反射遍历所有字段，执行验证
func validateStruct(obj interface{}, config BindingConfig) error {
    // 如果对象实现了Validator接口，调用其Validate方法
    if validator, ok := obj.(Validator); ok {
        if err := validator.Validate(); err != nil {
            return err
        }
    }

    // 使用反射验证结构体字段
    v := reflect.ValueOf(obj)
    if v.Kind() != reflect.Ptr || v.IsNil() {
        return nil // 不是指针，跳过验证
    }
    v = v.Elem()

    if v.Kind() != reflect.Struct {
        return nil // 不是结构体，跳过验证
    }

    t := v.Type()
    errors := make(ValidationErrors)

    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        fieldV := v.Field(i)

        // 跳过非导出字段
        if !field.IsExported() {
            continue
        }

        // 获取tag
        tag := field.Tag.Get(config.TagName)
        if tag == "" {
            continue
        }

        parts := parseTag(tag)

        // 检查required
        if _, ok := parts["required"]; ok {
            if isEmpty(fieldV) {
                errors.Add(field.Name, fmt.Errorf("field is required"))
                continue
            }
        }

        // 执行其他验证规则
        for rule, arg := range parts {
            if rule == "required" || rule == "name" {
                continue
            }

            // 获取字段值
            value, err := getFieldValue(fieldV)
            if err != nil {
                errors.Add(field.Name, err)
                continue
            }

            // 验证
            if err := validateField(field, rule, arg, value); err != nil {
                errors.Add(field.Name, err)
            }
        }
    }

    if len(errors) > 0 {
        return errors
    }

    return nil
}

// isEmpty：检查字段是否为空
func isEmpty(field reflect.Value) bool {
    switch field.Kind() {
    case reflect.String:
        return field.String() == ""
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
        return field.Int() == 0
    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        return field.Uint() == 0
    case reflect.Float32, reflect.Float64:
        return field.Float() == 0
    case reflect.Bool:
        return !field.Bool()
    case reflect.Slice, reflect.Array, reflect.Map:
        return field.Len() == 0
    case reflect.Ptr, reflect.Interface:
        return field.IsNil()
    default:
        return false
    }
}

// getFieldValue：获取字段值（返回[]byte）
func getFieldValue(field reflect.Value) ([]byte, error) {
    switch field.Kind() {
    case reflect.String:
        return []byte(field.String()), nil
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
        return []byte(strconv.FormatInt(field.Int(), 10)), nil
    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        return []byte(strconv.FormatUint(field.Uint(), 10)), nil
    case reflect.Float32, reflect.Float64:
        return []byte(strconv.FormatFloat(field.Float(), 'f', -1, 64)), nil
    case reflect.Bool:
        return []byte(strconv.FormatBool(field.Bool())), nil
    default:
        return nil, fmt.Errorf("unsupported field type: %s", field.Kind())
    }
}
```

## 5. Context集成

### 5.1 自动绑定方法

```go
// Context添加绑定相关方法

// Bind：自动绑定（根据Content-Type选择绑定器）
func (c *Context) Bind(obj interface{}) error {
    contentType := c.ContentType()

    var binder Binder
    switch contentType {
    case "application/json":
        binder = NewJSONBinder(DefaultBindingConfig)
    case "application/xml", "text/xml":
        binder = NewXMLBinder(DefaultBindingConfig)
    case "application/x-www-form-urlencoded":
        binder = NewFormBinder(DefaultBindingConfig)
    case "multipart/form-data":
        binder = NewMultipartBinder(DefaultBindingConfig)
    default:
        // 默认JSON
        binder = NewJSONBinder(DefaultBindingConfig)
    }

    return binder.Bind(c, obj)
}

// BindJSON：JSON绑定（带验证）
func (c *Context) BindJSON(obj interface{}) error {
    return NewJSONBinder(DefaultBindingConfig).Bind(c, obj)
}

// BindXML：XML绑定（带验证）
func (c *Context) BindXML(obj interface{}) error {
    return NewXMLBinder(DefaultBindingConfig).Bind(c, obj)
}

// BindForm：Form绑定（带验证）
func (c *Context) BindForm(obj interface{}) error {
    return NewFormBinder(DefaultBindingConfig).Bind(c, obj)
}

// BindMultipart：Multipart绑定（带验证）
func (c *Context) BindMultipart(obj interface{}) error {
    return NewMultipartBinder(DefaultBindingConfig).Bind(c, obj)
}

// ShouldBind：绑定但不自动响应错误
func (c *Context) ShouldBind(obj interface{}) error {
    return c.Bind(obj)
}

// ShouldBindJSON：JSON绑定但不自动响应错误
func (c *Context) ShouldBindJSON(obj interface{}) error {
    return c.BindJSON(obj)
}

// BindAndValidate：绑定并验证（与Bind相同，更语义化）
func (c *Context) BindAndValidate(obj interface{}) error {
    return c.Bind(obj)
}

// BindWithConfig：使用自定义配置绑定
func (c *Context) BindWithConfig(obj interface{}, cfg BindingConfig) error {
    contentType := c.ContentType()

    var binder Binder
    switch contentType {
    case "application/json":
        binder = NewJSONBinder(cfg)
    case "application/xml", "text/xml":
        binder = NewXMLBinder(cfg)
    case "application/x-www-form-urlencoded":
        binder = NewFormBinder(cfg)
    case "multipart/form-data":
        binder = NewMultipartBinder(cfg)
    default:
        // 默认JSON
        binder = NewJSONBinder(cfg)
    }

    return binder.Bind(c, obj)
}
```

## 6. 使用示例

### 6.1 基本使用

#### 示例1：JSON绑定

```go
// 定义请求结构体
type CreateUserRequest struct {
    Name     string `json:"name" bind:"required;min=3;max=50"`
    Email    string `json:"email" bind:"required;email"`
    Age      int    `json:"age" bind:"min=18;max=120"`
    Password string `json:"password" bind:"required;min=8"`
}

// Handler
func createUser(ctx *Context) Result {
    var req CreateUserRequest
    if err := ctx.BindJSON(&req); err != nil {
        // 处理验证错误
        if vErrs, ok := err.(ValidationErrors); ok {
            return AutoCode(map[string]interface{}{
                "error": "validation failed",
                "fields": vErrs,
            }, http.StatusBadRequest)
        }
        return Error(err)
    }

    // 创建用户...
    user := User{
        ID:    1,
        Name:  req.Name,
        Email: req.Email,
    }

    return AutoCode(user, http.StatusCreated)
}

server.POST("/users", Wrap(createUser))
```

**请求**：
```json
POST /users
Content-Type: application/json

{
  "name": "John",
  "email": "john@example.com",
  "age": 25,
  "password": "password123"
}
```

**响应**：
```json
{
  "id": 1,
  "name": "John",
  "email": "john@example.com"
}
```

**错误请求**：
```json
{
  "name": "Jo",
  "email": "invalid-email",
  "age": 15,
  "password": "short"
}
```

**错误响应**：
```json
{
  "error": "validation failed",
  "fields": {
    "Name": "value length must be >= 3",
    "Email": "invalid email format",
    "Age": "value must be >= 18",
    "Password": "value length must be >= 8"
  }
}
```

#### 示例2：自动绑定

```go
func createUser(ctx *Context) Result {
    var req CreateUserRequest
    if err := ctx.Bind(&req); err != nil {
        return AutoCode(map[string]string{"error": err.Error()}, http.StatusBadRequest)
    }

    // 创建用户...
    return Auto(User{ID: 1, Name: req.Name})
}

server.POST("/users", Wrap(createUser))
```

**根据Content-Type自动选择绑定器**：
- `Content-Type: application/json` -> JSONBinder
- `Content-Type: application/xml` -> XMLBinder
- `Content-Type: application/x-www-form-urlencoded` -> FormBinder
- `Content-Type: multipart/form-data` -> MultipartBinder

### 6.2 验证规则示例

#### 示例3：多种验证规则

```go
type UpdateUserRequest struct {
    Name    string `bind:"required;min=3;max=50"`
    Email   string `bind:"required;email"`
    Phone   string `bind:"len=11;pattern=^1[3-9]\\d{9}$"`
    Role    string `bind:"oneof=admin,user,guest"`
    Website string `bind:"url"`
}
```

#### 示例4：自定义字段名

```go
type LoginRequest struct {
    Username string `bind:"required;name=username"`
    Password string `bind:"required;name=password"`
}

// JSON: {"username": "john", "password": "secret"}
// Form: username=john&password=secret
```

### 6.3 文件上传

#### 示例5：Multipart绑定

```go
type UploadRequest struct {
    Title    string                 `bind:"required;name=title"`
    Category string                 `bind:"required;name=category"`
    File     *multipart.FileHeader  `bind:"required;name=file"`
}

func uploadFile(ctx *Context) Result {
    var req UploadRequest
    if err := ctx.BindMultipart(&req); err != nil {
        return AutoCode(map[string]string{"error": err.Error()}, http.StatusBadRequest)
    }

    // 保存文件
    dst, err := os.Create("uploads/" + req.File.Filename)
    if err != nil {
        return Error(err)
    }
    defer dst.Close()

    src, err := req.File.Open()
    if err != nil {
        return Error(err)
    }
    defer src.Close()

    if _, err := io.Copy(dst, src); err != nil {
        return Error(err)
    }

    return Auto(map[string]string{
        "message":  "file uploaded",
        "filename": req.File.Filename,
        "size":     strconv.FormatInt(int64(req.File.Size), 10),
    })
}

server.POST("/upload", Wrap(uploadFile))
```

### 6.4 自定义验证

#### 示例6：实现Validator接口

```go
type User struct {
    ID       int    `bind:"required"`
    Name     string `bind:"required;min=3"`
    Email    string `bind:"required;email"`
    Password string `bind:"required;min=8"`
}

// 自定义验证逻辑
func (u *User) Validate() error {
    // 邮箱唯一性检查
    if exists, _ := db.EmailExists(u.Email); exists {
        return fmt.Errorf("email already exists")
    }

    // 密码强度检查
    if !isStrongPassword(u.Password) {
        return fmt.Errorf("password is not strong enough")
    }

    return nil
}

func updateUser(ctx *Context) Result {
    var user User
    if err := ctx.Bind(&user); err != nil {
        return AutoCode(map[string]string{"error": err.Error()}, http.StatusBadRequest)
    }

    // 会自动调用user.Validate()
    // 更新用户...
    return Auto(user)
}

server.PUT("/users/:id", Wrap(updateUser))
```

### 6.5 配置示例

#### 示例7：自定义配置

```go
func createUser(ctx *Context) Result {
    cfg := BindingConfig{
        TagName:    "validate",
        StrictMode: true,
        MaxBodySize: 10 << 20, // 10MB
        SkipEmpty:  true,
    }

    var req CreateUserRequest
    if err := ctx.BindWithConfig(&req, cfg); err != nil {
        return AutoCode(map[string]string{"error": err.Error()}, http.StatusBadRequest)
    }

    return Auto(User{ID: 1, Name: req.Name})
}

server.POST("/users", Wrap(createUser))
```

#### 示例8：仅绑定不验证

```go
func createUser(ctx *Context) Result {
    cfg := BindingConfig{
        DisableAuto: true, // 禁用自动验证
    }

    var req CreateUserRequest
    if err := ctx.BindWithConfig(&req, cfg); err != nil {
        return AutoCode(map[string]string{"error": err.Error()}, http.StatusBadRequest)
    }

    // 手动验证
    if req.Name == "" {
        return AutoCode(map[string]string{"error": "name is required"}, http.StatusBadRequest)
    }

    return Auto(User{ID: 1, Name: req.Name})
}
```

### 6.6 RESTful API示例

#### 示例9：完整的CRUD API

```go
// GET /users - 获取用户列表
func listUsers(ctx *Context) Result {
    // 不需要绑定
    users := []User{
        {ID: 1, Name: "John", Email: "john@example.com"},
        {ID: 2, Name: "Jane", Email: "jane@example.com"},
    }
    return Auto(users)
}

// GET /users/:id - 获取单个用户
func getUser(ctx *Context) Result {
    // 不需要绑定
    user, err := findUser(ctx.Param("id"))
    if err != nil {
        return ErrorCode(err, http.StatusNotFound)
    }
    return Auto(user)
}

// POST /users - 创建用户
type CreateUserRequest struct {
    Name     string `bind:"required;min=3;max=50"`
    Email    string `bind:"required;email"`
    Password string `bind:"required;min=8"`
}

func createUser(ctx *Context) Result {
    var req CreateUserRequest
    if err := ctx.Bind(&req); err != nil {
        if vErrs, ok := err.(ValidationErrors); ok {
            return AutoCode(map[string]interface{}{
                "error":  "validation failed",
                "fields": vErrs,
            }, http.StatusBadRequest)
        }
        return Error(err)
    }

    user, err := saveUser(req)
    if err != nil {
        return Error(err)
    }

    return AutoCode(user, http.StatusCreated).
        WithHeader("Location", fmt.Sprintf("/users/%d", user.ID))
}

// PUT /users/:id - 更新用户
type UpdateUserRequest struct {
    Name  string `bind:"required;min=3;max=50"`
    Email string `bind:"required;email"`
}

func updateUser(ctx *Context) Result {
    var req UpdateUserRequest
    if err := ctx.Bind(&req); err != nil {
        if vErrs, ok := err.(ValidationErrors); ok {
            return AutoCode(map[string]interface{}{
                "error":  "validation failed",
                "fields": vErrs,
            }, http.StatusBadRequest)
        }
        return Error(err)
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
    // 不需要绑定
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

## 7. 零反射优化（代码生成）

### 7.1 问题分析

**当前实现**：
- 每次绑定都使用`reflect.ValueOf`遍历结构体字段
- 每次验证都使用`reflect.Value`获取字段值
- 反射开销较大（~100-500ns per field）

**优化方案**：
- 使用代码生成，为每个结构体生成专用的绑定函数
- 编译时确定字段类型，运行时无反射

### 7.2 代码生成设计

#### 示例：生成的绑定函数

```go
// 生成前（使用反射）：
func validateStruct(obj interface{}, config BindingConfig) error {
    v := reflect.ValueOf(obj)
    // ... 反射遍历字段
}

// 生成后（无反射）：
func validateCreateUserRequest(req *CreateUserRequest) error {
    errors := make(ValidationErrors)

    // Name字段
    if req.Name == "" {
        errors["Name"] = fmt.Errorf("field is required")
    } else if len(req.Name) < 3 {
        errors["Name"] = fmt.Errorf("value length must be >= 3")
    } else if len(req.Name) > 50 {
        errors["Name"] = fmt.Errorf("value length must be <= 50")
    }

    // Email字段
    if req.Email == "" {
        errors["Email"] = fmt.Errorf("field is required")
    } else if !isValidEmail(req.Email) {
        errors["Email"] = fmt.Errorf("invalid email format")
    }

    // Age字段
    if req.Age < 18 {
        errors["Age"] = fmt.Errorf("value must be >= 18")
    } else if req.Age > 120 {
        errors["Age"] = fmt.Errorf("value must be <= 120")
    }

    // Password字段
    if req.Password == "" {
        errors["Password"] = fmt.Errorf("field is required")
    } else if len(req.Password) < 8 {
        errors["Password"] = fmt.Errorf("value length must be >= 8")
    }

    if len(errors) > 0 {
        return errors
    }

    // 调用自定义Validate方法
    return req.Validate()
}
```

### 7.3 代码生成器设计

```go
// gkbind代码生成器
// 用法：
//   //go:generate gkbind -type=CreateUserRequest
//   type CreateUserRequest struct {
//       Name string `bind:"required;min=3"`
//   }

package main

import (
    "flag"
    "fmt"
    "go/parser"
    "go/token"
    "strings"
)

func main() {
    // 解析命令行参数
    typeName := flag.String("type", "", "struct type name")
    output := flag.String("output", "", "output file")
    flag.Parse()

    if *typeName == "" {
        fmt.Println("error: -type is required")
        return
    }

    // 解析Go文件
    fset := token.NewFileSet()
    node, err := parser.ParseFile(fset, "", nil, parser.ParseComments)
    if err != nil {
        fmt.Printf("error: %v\n", err)
        return
    }

    // 生成绑定函数
    code := generateBinder(*typeName)

    // 输出到文件或stdout
    if *output != "" {
        err := os.WriteFile(*output, []byte(code), 0644)
        if err != nil {
            fmt.Printf("error: %v\n", err)
            return
        }
    } else {
        fmt.Println(code)
    }
}

func generateBinder(typeName string) string {
    // 实现代码生成逻辑
    // 1. 解析结构体定义
    // 2. 遍历字段，提取验证规则
    // 3. 生成无反射的绑定函数
    // 4. 返回生成的代码

    return fmt.Sprintf(`// Code generated by gkbind; DO NOT EDIT.

package gserver

func validate%s(obj interface{}) error {
    // 生成的验证代码...
    return nil
}
`, typeName)
}
```

### 7.4 使用代码生成

```go
//go:generate gkbind -type=CreateUserRequest
type CreateUserRequest struct {
    Name     string `bind:"required;min=3;max=50"`
    Email    string `bind:"required;email"`
    Age      int    `bind:"min=18;max=120"`
    Password string `bind:"required;min=8"`
}

// Handler中使用生成的函数
func createUser(ctx *Context) Result {
    var req CreateUserRequest
    if err := ctx.Bind(&req); err != nil {
        return AutoCode(map[string]string{"error": err.Error()}, http.StatusBadRequest)
    }

    // 自动调用生成的validateCreateUserRequest
    return Auto(User{ID: 1, Name: req.Name})
}
```

## 8. 性能优化策略

### 8.1 基准测试对比

```go
// 测试1：反射验证
func Benchmark_ReflectValidation(b *testing.B) {
    req := CreateUserRequest{Name: "John", Email: "john@example.com", Age: 25, Password: "password123"}

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        validateStruct(&req, DefaultBindingConfig)
    }
}

// 测试2：代码生成验证
func Benchmark_CodeGenValidation(b *testing.B) {
    req := CreateUserRequest{Name: "John", Email: "john@example.com", Age: 25, Password: "password123"}

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        validateCreateUserRequest(&req)
    }
}

// 预期结果：
// ReflectValidation:  ~1000 ns/op, ~50 allocs/op
// CodeGenValidation:   ~100 ns/op, ~1 allocs/op (10x faster)
```

### 8.2 优化策略总结

| 优化点 | 策略 | 性能提升 |
|--------|------|----------|
| 反射 | 代码生成 | 10x |
| JSON解码 | 使用sonic/jsoniter | 2-3x |
| 字符串解析 | 预编译正则 | 2x |
| 内存分配 | sync.Pool | 减少50% |

## 9. 测试计划

### 9.1 单元测试

```go
func TestBindJSON(t *testing.T) {
    ctx := &Context{}
    ctx.fastCtx = &fasthttp.RequestCtx{}
    ctx.fastCtx.Request.SetBody([]byte(`{"name":"John","email":"john@example.com"}`))

    var req CreateUserRequest
    err := ctx.BindJSON(&req)

    assert.NoError(t, err)
    assert.Equal(t, "John", req.Name)
    assert.Equal(t, "john@example.com", req.Email)
}

func TestValidationErrors(t *testing.T) {
    ctx := &Context{}
    ctx.fastCtx = &fasthttp.RequestCtx{}
    ctx.fastCtx.Request.SetBody([]byte(`{"name":"Jo","email":"invalid"}`))

    var req CreateUserRequest
    err := ctx.BindJSON(&req)

    assert.Error(t, err)
    vErrs, ok := err.(ValidationErrors)
    assert.True(t, ok)
    assert.True(t, vErrs.Has("Name"))
    assert.True(t, vErrs.Has("Email"))
}
```

### 9.2 集成测试

```go
func TestBindIntegration(t *testing.T) {
    server := NewServer()
    server.POST("/users", Wrap(func(ctx *Context) Result {
        var req CreateUserRequest
        if err := ctx.Bind(&req); err != nil {
            return AutoCode(map[string]string{"error": err.Error()}, http.StatusBadRequest)
        }
        return Auto(User{ID: 1, Name: req.Name})
    }))

    // 测试POST请求...
}
```

## 10. 总结

### 10.1 设计优势
1. **字段级验证**：支持丰富的验证规则
2. **零反射优化**：代码生成技术实现10x性能提升
3. **自动Content-Type选择**：根据Content-Type自动选择绑定器
4. **简洁易用**：结构体标签驱动，类似Spring Boot
5. **高性能**：支持零分配绑定路径

### 10.2 内置验证规则

| 规则 | 参数 | 说明 | 示例 |
|------|------|------|------|
| required | - | 字段必填 | `bind:"required"` |
| min | number | 最小值/最小长度 | `bind:"min=3"` |
| max | number | 最大值/最大长度 | `bind:"max=50"` |
| len | number | 固定长度 | `bind:"len=11"` |
| email | - | 邮箱格式 | `bind:"email"` |
| url | - | URL格式 | `bind:"url"` |
| pattern | regex | 正则匹配 | `bind:"pattern=^[a-z]+$"` |
| oneof | list | 枚举值 | `bind:"oneof=admin,user"` |

### 10.3 实施步骤
1. 实现Binder接口和内置Binder
2. 实现验证规则和ValidationErrors
3. Context添加绑定方法
4. 编写单元测试
5. 编写集成测试
6. 编写基准测试
7. 实现代码生成器（可选，后续优化）
8. 更新文档和示例

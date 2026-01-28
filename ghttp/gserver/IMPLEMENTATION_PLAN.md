# gserver 重新设计 - 完整实施计划

## 概述

本文档总结了gserver的完整重新设计方案，包括Result接口、Binding系统、中间件Hooks、Context API、模板渲染和路由优化。所有设计均遵循以下原则：

1. **不向后兼容**：完全重新设计，不留历史包袱
2. **工程化**：符合Go最佳实践，清晰的分层架构
3. **性能优先**：零额外开销，关键路径优化
4. **Spring Boot风格**：简化开发，减少模板代码

## 1. Result接口设计 (DESIGN_RESULT_v2.md)

### 核心特性

- **AutoResult自动marshal**：根据Accept header自动选择JSON/XML编码
- **不检测isLast**：Result总是执行，由用户控制handler链
- **链式调用**：`Auto(data).WithCode(201).WithHeader("Location", "/users/1")`

### 设计文档

- `DESIGN_RESULT_v2.md`：完整设计文档

### 实施步骤

1. 实现`AutoResult`及相关便捷函数（`Auto`, `AutoCode`, `AutoWithHeaders`）
2. 实现其他Result类型（`JSONResult`, `XMLResult`, `HTMLResult`, `StringResult`等）
3. 编写单元测试
4. 编写集成测试
5. 编写基准测试
6. 更新文档和示例

### 预期性能

- **热路径开销**：~2-5ns（可忽略）
- **内存分配**：与直接调用`ctx.JSON`相同

---

## 2. Binding系统设计 (DESIGN_BINDING.md)

### 核心特性

- **字段级验证**：支持结构体标签驱动（`bind:"required;min=3;max=50"`）
- **零反射优化**：代码生成技术实现10x性能提升（可选，后续优化）
- **自动Content-Type选择**：根据Content-Type header自动选择绑定器（JSON/XML/Form/Multipart）
- **自定义验证**：支持`Validator`接口

### 设计文档

- `DESIGN_BINDING.md`：完整设计文档

### 内置验证规则

| 规则 | 参数 | 说明 |
|------|------|------|
| required | - | 字段必填 |
| min | number | 最小值/最小长度 |
| max | number | 最大值/最大长度 |
| len | number | 固定长度 |
| email | - | 邮箱格式 |
| url | - | URL格式 |
| pattern | regex | 正则匹配 |
| oneof | list | 枚举值 |

### 实施步骤

1. 实现`Binder`接口和内置Binder（`JSONBinder`, `XMLBinder`, `FormBinder`, `MultipartBinder`）
2. 实现验证规则和`ValidationErrors`
3. Context添加绑定方法（`Bind`, `BindJSON`, `BindXML`, `BindForm`, `BindMultipart`）
4. 编写单元测试
5. 编写集成测试
6. 编写基准测试
7. （可选，后续）实现代码生成器（`gkbind`）
8. 更新文档和示例

### 预期性能

- **反射验证**：~1000 ns/op, ~50 allocs/op
- **代码生成验证**：~100 ns/op, ~1 allocs/op（10x提升）

---

## 3. 中间件Hooks系统 (DESIGN_HOOKS.md)

### 核心特性

- **Before/After/Panic Hooks**：完整的生命周期支持
- **Cancellable Hooks**：hooks可以取消请求
- **动态配置**：运行时添加/移除hooks（全局或路由特定）
- **简洁API**：链式调用，`WrapWithHooks(handler).AddBefore(...).AddAfter(...)`

### 设计文档

- `DESIGN_HOOKS.md`：完整设计文档

### 实施步骤

1. 实现`Hook`接口和`HookFunc`
2. 实现`Hooks`集合和`HooksManager`
3. 实现`MiddlewareWithHooks`
4. 改造现有中间件（`RequestIDWithHooks`, `CORSWithHooks`, `RequestLoggerWithHooks`, `RecoveryWithHooks`）
5. Server集成`HooksManager`
6. 编写单元测试
7. 编写集成测试
8. 更新文档和示例

### 预期性能

- **Hook执行开销**：每个Hook~2ns
- **Before+After Hooks总开销**：~4ns（可忽略）

---

## 4. Context API完整对齐 (DESIGN_CONTEXT_API.md)

### 核心特性

- **API对齐**：对齐gin/hertz/fiber核心方法
- **不是wrapper**：直接实现，不使用compat wrapper
- **零额外开销**：新增方法性能影响可忽略

### 新增方法汇总

| 优先级 | 方法 | 说明 |
|--------|------|------|
| 高 | `Get(key interface{}) (interface{}, bool)` | 获取Context存储值 |
| 高 | `MustGet(key interface{}) interface{}` | 获取Context存储值（panic if not exists） |
| 高 | `DefaultQuery(key, defaultValue string) string` | 获取Query参数（带默认值） |
| 高 | `DefaultPostForm(key, defaultValue string) string` | 获取PostForm参数（带默认值） |
| 高 | `Redirect(code int, location string)` | 重定向 |
| 中 | `GetRawData() ([]byte, error)` | 获取原始请求body |
| 中 | `GetCookie(name string) (string, error)` | 获取Cookie（返回error） |
| 中 | `File(filepath string)` | 返回文件 |
| 中 | `Stream(r io.Reader, contentType string)` | 流式响应 |
| 低 | `FileAttachment(filepath, filename string)` | 返回文件（指定下载名） |

### 设计文档

- `DESIGN_CONTEXT_API.md`：完整设计文档

### 实施步骤

1. 实现`Get`/`MustGet`方法
2. 实现`DefaultQuery`/`DefaultPostForm`方法
3. 实现`Redirect`方法
4. 实现`GetRawData`方法
5. 实现`GetCookie`方法（改进）
6. 实现`File`方法
7. 实现`Stream`方法
8. （可选）实现`FileAttachment`方法
9. 编写单元测试
10. 编写集成测试
11. 更新文档和示例

### 预期性能

- **所有新增方法开销**：可忽略（~2-50ns）

---

## 5. 模板渲染系统 (DESIGN_TEMPLATES.md)

### 核心特性

- **多引擎支持**：支持html/template、text/template、第三方模板引擎
- **热重载**：开发环境自动重新加载修改的模板
- **静态文件服务**：完整的静态文件服务（缓存、Range、压缩）
- **高性能**：生产环境下模板预编译、静态文件缓存

### 设计文档

- `DESIGN_TEMPLATES.md`：完整设计文档

### 实施步骤

1. 实现`TemplateEngine`接口
2. 实现`GoTemplateEngine`（html/template + text/template）
3. 实现`HotReloadRenderer`
4. 实现`StaticFileServer`
5. Server集成（`WithTemplateEngine`, `WithStaticFileServer`）
6. 编写单元测试
7. 编写集成测试
8. 更新文档和示例

### 预期性能

| 操作 | 性能 | 说明 |
|------|------|------|
| 模板渲染（缓存） | ~10μs | 预编译模板 |
| 静态文件（缓存） | ~5μs | 内存缓存 |

---

## 6. 路由匹配优化 (DESIGN_ROUTING.md)

### 核心特性

- **智能优先级**：根据访问频率动态调整路由匹配顺序
- **路由预热**：启动时预热常用路由
- **路径解析缓存**：缓存已解析的路径参数
- **正则预编译**：预编译所有正则路由

### 设计文档

- `DESIGN_ROUTING.md`：完整设计文档

### 实施步骤

1. 实现`RouteStats`（路由统计）
2. 实现`PathParamsCache`（路径参数缓存）
3. 实现`RegexCache`（正则表达式缓存）
4. 实现`SmartMatcher`（智能路由匹配器）
5. 实现`RouteWarmer`（路由预热器）
6. Server集成（`WithRouteOptimization`, `WithHotRouteThreshold`, `WithParamsCacheSize`）
7. 编写单元测试
8. 编写集成测试
9. 编写基准测试
10. 更新文档和示例

### 预期性能提升

| 场景 | 优化前 | 优化后 | 提升 |
|------|--------|--------|------|
| 热路由 | ~100ns | ~80ns | 20% |
| 已缓存路由 | ~100ns | ~50ns | 50% |
| 正则路由 | ~500ns | ~100ns | 80% |

---

## 7. 实施计划

### 7.1 Phase 1：核心功能（高优先级）

**目标**：实现Result接口和Binding系统，这是最重要的改进。

**任务**：
1. Result接口实现（DESIGN_RESULT_v2.md）
   - AutoResult及相关便捷函数
   - 其他Result类型（JSON, XML, HTML, String等）
   - 单元测试、集成测试、基准测试

2. Binding系统实现（DESIGN_BINDING.md）
   - Binder接口和内置Binder
   - 验证规则和ValidationErrors
   - Context绑定方法
   - 单元测试、集成测试、基准测试

**预计时间**：2-3周

### 7.2 Phase 2：Context API对齐（中优先级）

**目标**：对齐gin/hertz/fiber核心Context方法。

**任务**：
1. 实现新增的Context方法（DESIGN_CONTEXT_API.md）
   - Get/MustGet
   - DefaultQuery/DefaultPostForm
   - Redirect
   - GetRawData
   - GetCookie（改进）
   - File
   - Stream
   - 单元测试、集成测试

**预计时间**：1周

### 7.3 Phase 3：中间件Hooks系统（中优先级）

**目标**：添加Before/After/Panic Hooks支持。

**任务**：
1. Hook接口实现（DESIGN_HOOKS.md）
   - Hook接口和HookFunc
   - Hooks集合和HooksManager
   - MiddlewareWithHooks
   - 改造现有中间件
   - Server集成
   - 单元测试、集成测试

**预计时间**：1周

### 7.4 Phase 4：模板渲染和路由优化（低优先级）

**目标**：完善模板渲染和路由匹配优化。

**任务**：
1. 模板渲染系统（DESIGN_TEMPLATES.md）
   - TemplateEngine接口
   - GoTemplateEngine
   - HotReloadRenderer
   - StaticFileServer
   - Server集成
   - 单元测试、集成测试

2. 路由匹配优化（DESIGN_ROUTING.md）
   - RouteStats
   - PathParamsCache
   - RegexCache
   - SmartMatcher
   - RouteWarmer
   - Server集成
   - 单元测试、集成测试、基准测试

**预计时间**：2-3周

### 7.5 Phase 5：文档和示例

**目标**：完善文档和示例。

**任务**：
1. 更新README
2. 编写使用示例
3. 编写最佳实践
4. 性能调优指南

**预计时间**：1周

---

## 8. 总结

### 8.1 设计文档汇总

| 设计文档 | 核心特性 | 优先级 |
|---------|---------|--------|
| DESIGN_RESULT_v2.md | AutoResult自动marshal | 高 |
| DESIGN_BINDING.md | 字段级验证、零反射优化 | 高 |
| DESIGN_HOOKS.md | Before/After/Panic Hooks | 中 |
| DESIGN_CONTEXT_API.md | API对齐gin/hertz/fiber | 中 |
| DESIGN_TEMPLATES.md | 多引擎、热重载、静态文件 | 中 |
| DESIGN_ROUTING.md | 智能优先级、路由预热 | 低 |

### 8.2 关键改进

1. **Spring Boot风格**：handler直接返回数据，减少模板代码
2. **字段级验证**：结构体标签驱动，类似Spring Boot的`@Valid`
3. **完整Context API**：对齐gin/hertz/fiber核心方法
4. **中间件Hooks**：Before/After/Panic支持，灵活的中间件扩展
5. **模板渲染**：多引擎、热重载、静态文件服务
6. **路由优化**：智能优先级、路由预热、路径解析缓存

### 8.3 性能提升

| 优化项 | 提升 | 说明 |
|--------|------|------|
| Result接口 | 0% | 热路径零额外开销 |
| Binding系统 | 10x | 代码生成验证（可选） |
| Hooks系统 | 0% | Hook执行可忽略 |
| Context API | 0% | 新增方法可忽略 |
| 模板渲染 | 10x | 模板预编译 |
| 路由匹配 | 20-80% | 热路由、缓存、正则预编译 |

### 8.4 预计总时间

- **Phase 1**：2-3周（核心功能）
- **Phase 2**：1周（Context API）
- **Phase 3**：1周（Hooks系统）
- **Phase 4**：2-3周（模板渲染和路由优化）
- **Phase 5**：1周（文档和示例）

**总计**：7-9周

### 8.5 后续优化（可选）

1. **Binding代码生成**：实现`gkbind`工具，为结构体生成无反射验证函数
2. **第三方模板引擎**：支持pongo2、quicktemplate等
3. **路由自动预热**：基于访问历史自动选择要预热的路由
4. **性能监控**：添加Prometheus metrics导出
5. **分布式追踪**：集成OpenTelemetry

---

## 9. 开始实施

### 9.1 第一步：Result接口实现

从`DESIGN_RESULT_v2.md`开始实现，因为这是最重要的改进，也是其他功能的基础。

**建议顺序**：
1. 先实现`AutoResult`和核心Result类型
2. 编写单元测试验证基本功能
3. 编写集成测试验证整个流程
4. 编写基准测试验证性能
5. 更新文档和示例

### 9.2 开发环境设置

```bash
# 克隆仓库
git clone https://github.com/sofiworker/gk.git
cd gk/ghttp/gserver

# 运行测试
go test -v ./...

# 运行基准测试
go test -bench=. -benchmem ./...
```

### 9.3 提交规范

遵循Conventional Commits规范：
```
feat: 添加AutoResult支持Spring Boot风格返回值
fix: 修复Context.Get方法未正确处理nil的情况
chore: 更新设计文档
docs: 添加Result接口使用示例
```

---

## 10. 联系和支持

如有问题或建议，请：
1. 提交GitHub Issue
2. 发送Pull Request
3. 参考设计文档

---

**设计完成！准备开始实施！** 🚀

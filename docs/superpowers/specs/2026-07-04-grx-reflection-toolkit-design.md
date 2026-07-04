# grx 反射工具包设计

> 日期：2026-07-04
> 范围：增强 `github.com/sofiworker/gk/grx`，把它从薄的反射 helper 扩展为可复用的反射元数据、字段访问和结构体绑定底座。
> 参考源码：`github.com/go-playground/validator/v10@v10.23.0/cache.go`、`github.com/jmoiron/sqlx@v1.4.0/reflectx/reflect.go`。

---

## 1. 背景

当前 `grx` 主要提供：

- `FieldCache`：缓存 `reflect.Type` 到字段、方法和 tag 索引。
- `StructFieldInfo`：保存 `reflect.StructField` 与 `Index []int`。
- 字段名/tag 查找、方法查找与调用。
- `IsEmpty`、`SetValue`、`FastIndirect`、`UnsafeReflectValue` 等基础工具。

这些能力能减少一些重复反射，但还没有形成可复用的结构体元数据模型。上层包如果要做 HTTP 参数绑定、SQL scan、配置绑定、map 到 struct、批量字段访问，仍然要重复处理 tag 解析、嵌入字段遍历、冲突规则、nil 指针分配、字段路径预编译等问题。

`grx` 的目标应当是一个小而稳定的公共底座：把结构体解析和字段访问的成本前移到注册期或首次使用期，并把运行时热路径压缩为缓存查找和预编译 traversal。

---

## 2. 当前问题

### 2.1 抽象层次偏低

`FieldCache` 只暴露字段名到 `StructFieldInfo` 的映射。它无法表达：

- 字段路径，如 `User.Address.City`。
- 显式 tag 名称、tag options、原始 tag。
- 字段是否导出、匿名、嵌入、冲突、跳过。
- 同一结构体面向不同 tag/name 策略时的独立映射。
- 批量名字到 traversal 的预编译结果。

### 2.2 嵌入字段规则不完整

当前 `collectStructFields` 只递归匿名且类型为 struct 的字段：

- 不处理 `*Embedded` 指针嵌入。
- 不保留 BFS 优先级，直接 map 覆盖会让冲突行为不稳定。
- 不记录 shadowing 或 ambiguous field。
- 不区分直接字段、嵌入字段、带 tag 的匿名字段。

### 2.3 tag 能力薄弱

当前 tag 查找只截断逗号前的名字：

- 没有 `omitempty`、`inline`、`squash`、`required` 等 option 解析。
- 没有 tag name mapper，例如 `json:"user_id,omitempty"` 中先取 `user_id` 再做规范化。
- 没有统一的 `TagOptions` 查询接口。

### 2.4 热路径仍依赖反射细节

虽然字段列表被缓存，但使用方仍需要在每次处理时做：

- name/tag 到 index 的查找。
- `reflect.Indirect` 与 nil 指针处理。
- 不可设置字段判断。
- 批量字段顺序匹配。

缺少 `FieldByIndexesReadOnly`、`FieldByIndexesAlloc`、`TraversalsByName` 等面向热路径的 API。

### 2.5 兼容 API 暴露了可变内部状态

`GetCachedStructFields` 返回内部 map，调用方可以修改缓存内容。这不利于后续把缓存做成不可变结构。

---

## 3. 参考实现可借鉴点

### 3.1 validator 的缓存和解析策略

`go-playground/validator` 的核心模式：

- 使用 `structCache` 和 `tagCache` 分离结构体缓存与 tag 缓存。
- 缓存 map 存在 `atomic.Value` 中，读路径只做原子加载和 map 查询。
- 写路径加锁，copy-on-write 生成新 map，再整体 `Store`，使已发布缓存不可变。
- 结构体第一次解析时生成 `cStruct`、`cField`、`cTag`，运行时只走已编译链。
- tag 字符串解析为链表结构，避免每次校验重复 split/parse。
- 支持自定义字段名函数，结构体字段名和用户可见名分开保存。

对 `grx` 的启发：

- `StructMap` 应该是不可变对象，发布后不再修改。
- `Mapper` 的读路径应尽量无锁。
- tag/options 应在结构体解析期解析完成。
- 不同 tag/name 策略需要不同 `Mapper`，不能只按 `reflect.Type` 做全局缓存。

### 3.2 sqlx/reflectx 的结构体映射模型

`sqlx/reflectx` 的核心模式：

- `Mapper` 管理 tag 名称、字段名映射函数和类型缓存。
- `StructMap` 同时保存树、索引列表、路径索引、名称索引。
- `FieldInfo` 保存字段、index、path、name、options、parent/children。
- 用 BFS 构建映射，处理嵌入字段的优先级和 shadowing。
- `TraversalsByName` 把一批列名预编译成 index traversal，适合 SQL scan。
- `FieldByIndexes` 在写入路径上自动分配 nil pointer 和 nil map。
- `FieldByIndexesReadOnly` 在读取路径上不分配，避免读操作产生副作用。

对 `grx` 的启发：

- `grx` 应引入 `Mapper` / `StructMap` / `FieldInfo` 三层模型。
- 同一个 `StructMap` 应同时服务单字段查找、批量查找、路径查找和绑定。
- 读写访问路径要分开，避免只读逻辑意外分配。

---

## 4. 设计目标

### 4.1 必须支持

- 面向不同 tag/name 策略的 `Mapper`。
- 不可变 `StructMap` 元数据。
- 完整记录字段名、路径、tag、options、index、导出状态、嵌入状态。
- BFS 嵌入字段遍历与稳定冲突规则。
- 只读字段访问和自动分配写入访问。
- 批量名字到 traversal 的预编译。
- 保持现有 `FieldCache`、`LookupFieldInfo`、`LookupFieldByTag`、`Fields`、`Methods` 基本兼容。
- 给 ghttp、gsql、config/map binding 留出复用入口。

### 4.2 不做

- 不实现完整 validator DSL。
- 不实现 ORM。
- 不默认使用 unsafe 读写未导出字段。
- 不把所有绑定、转换、扫描逻辑塞进一个大文件。
- 不为性能目标引入外部依赖。

---

## 5. 推荐架构

### 5.1 文件划分

| 文件 | 责任 |
| --- | --- |
| `grx/mapper.go` | `Mapper`、`MapperOption`、类型缓存、默认 mapper。 |
| `grx/struct_map.go` | `StructMap`、`FieldInfo`、BFS 构图、冲突解决。 |
| `grx/tag.go` | `Tag`、`TagOptions`、tag/name 解析。 |
| `grx/traversal.go` | `FieldByIndexesReadOnly`、`FieldByIndexesAlloc`、`TraversalsByName`。 |
| `grx/bind.go` | map/value 到 struct 的轻量绑定，作为第二阶段能力。 |
| `grx/errors.go` | 公共错误与字段错误类型。 |
| `grx/reflect.go` | 保留现有兼容 API，逐步委托给新实现。 |
| `grx/*_test.go` | 按文件就近测试。 |
| `grx/bench_test.go` | 缓存、traversal、绑定基准。 |

`reflect.go` 不再继续膨胀。新增能力按责任拆文件，避免以后所有反射逻辑都集中在一个文件中。

### 5.2 核心类型

建议 API 形态：

```go
type Mapper struct {
	// unexported fields
}

type MapperOption func(*mapperConfig)

func NewMapper(opts ...MapperOption) *Mapper
func NewMapperForTag(tagName string) *Mapper

func WithTagName(name string) MapperOption
func WithNameMapper(fn func(string) string) MapperOption
func WithTagMapper(fn func(string) string) MapperOption
func WithIgnoreUnexported(ignore bool) MapperOption

func (m *Mapper) TypeMap(t reflect.Type) (*StructMap, error)
func (m *Mapper) MustTypeMap(t reflect.Type) *StructMap
func (m *Mapper) FieldByName(v reflect.Value, name string) (reflect.Value, bool)
func (m *Mapper) FieldByPath(v reflect.Value, path string) (reflect.Value, bool)
func (m *Mapper) TraversalsByName(t reflect.Type, names []string) ([][]int, error)
func (m *Mapper) TraversalsByNameFunc(t reflect.Type, names []string, fn func(int, []int) error) error
```

`NewMapperForTag("json")` 覆盖常见场景，`NewMapper(WithTagName("db"), WithNameMapper(SnakeCase))` 覆盖自定义场景。

`StructMap`：

```go
type StructMap struct {
	Type      reflect.Type
	Tree      *FieldInfo
	Fields    []*FieldInfo
	Names     map[string]*FieldInfo
	Paths     map[string]*FieldInfo
	Conflicts map[string][]*FieldInfo
}

func (sm *StructMap) LookupName(name string) (*FieldInfo, error)
func (sm *StructMap) LookupPath(path string) (*FieldInfo, error)
func (sm *StructMap) FieldByTraversal(index []int) *FieldInfo
```

`Names` 和 `Paths` 只包含已解决的字段；冲突字段放入 `Conflicts`，`LookupName` 返回上下文错误，避免静默选错字段。

`FieldInfo`：

```go
type FieldInfo struct {
	Field     reflect.StructField
	Index     []int
	Path      string
	Name      string
	Tag       Tag
	Options   TagOptions
	Exported  bool
	Anonymous bool
	Embedded  bool
	Depth     int
	Parent    *FieldInfo
	Children  []*FieldInfo
}
```

`Index` 发布前拷贝，发布后不可变。`Children` 只在构图阶段写入，发布后只读。

`Tag` 与 `TagOptions`：

```go
type Tag struct {
	Key     string
	Raw     string
	Name    string
	Options TagOptions
	Present bool
	Ignored bool
}

type TagOptions map[string]string

func (opts TagOptions) Has(name string) bool
func (opts TagOptions) Get(name string) (string, bool)
```

tag 解析遵循通用约定：

- `json:"-"` 跳过字段。
- `json:",omitempty"` 表示 tag 存在但名称为空，名称回退到字段名映射。
- `json:"name,omitempty"` 名称为 `name`，option 包含 `omitempty`。
- `tagMapFunc` 先作用于原始 tag 值，再拆分名称和 option。

### 5.3 访问 API

建议分清只读和写入路径：

```go
func FieldByIndexesReadOnly(v reflect.Value, indexes []int) (reflect.Value, error)
func FieldByIndexesAlloc(v reflect.Value, indexes []int) (reflect.Value, error)

type Accessor struct {
	Index []int
	Type  reflect.Type
}

func NewAccessor(index []int, typ reflect.Type) Accessor
func (a Accessor) Get(v reflect.Value) (reflect.Value, error)
func (a Accessor) Set(v reflect.Value, value any) error
```

只读路径不分配；写入路径会在可设置的情况下自动分配 nil pointer 和 nil map。`Set` 可以复用现有 `SetValue` 的 assign/convert 规则，但需要返回错误，不能静默失败。

### 5.4 轻量绑定 API

绑定能力放在第二阶段，建立在 `Mapper` 和 traversal 之上：

```go
type Binder struct {
	Mapper *Mapper
}

func NewBinder(mapper *Mapper) *Binder
func (b *Binder) BindMap(dst any, values map[string]any) error
func (b *Binder) BindStringMap(dst any, values map[string]string) error
```

第一版只支持明确、低风险的转换：

- string 到 string、bool、int/uint/float 基础类型。
- assignable / convertible 值。
- pointer 自动分配。
- 未找到字段默认忽略，严格模式后续通过 option 增加。

不在第一版支持复杂时间格式、slice 多值、嵌套 map、校验规则。避免重复 `mapstructure` 的完整功能。

---

## 6. 缓存策略

推荐采用 validator 风格的 copy-on-write 缓存：

```go
type typeMapCache struct {
	mu sync.Mutex
	v  atomic.Value // map[reflect.Type]*StructMap
}
```

初始化时存入空 map。读取流程：

1. `m := c.v.Load().(map[reflect.Type]*StructMap)`。
2. 查到直接返回。
3. 未查到进入 `mu`。
4. 双重检查。
5. 构建 `StructMap`。
6. copy 旧 map，加新项，`Store` 新 map。

理由：

- 结构体类型集合通常有限，读远多于写。
- 已发布 `StructMap` 不可变，适合无锁读。
- 比 `sync.Map` 更容易保证快照语义。
- 比全局 mutex 更适合热路径。

需要注意：缓存 key 不能只有 `reflect.Type`。不同 `Mapper` 的 tag/name 策略不同，所以缓存应挂在 `Mapper` 实例上。全局默认 mapper 可以使用默认字段名策略。

---

## 7. 字段遍历与冲突规则

构建 `StructMap` 使用 BFS：

1. 根类型必须 deref 到 struct，否则返回 `ErrNotStruct`。
2. 队列从根结构体开始。
3. 逐字段解析 tag、名称、options。
4. `-` 跳过。
5. 未导出非匿名字段默认跳过。
6. 匿名 struct 或 `*struct` 字段进入 BFS 队列。
7. 普通 struct 字段保留为路径节点，但默认不展开到 `Names`，除非后续明确支持深层 flatten。

名称冲突按以下优先级解决：

1. 深度浅的字段优先。
2. 同深度下显式 tag 优先于字段名映射。
3. 同深度、同显式性、同名时标记冲突，不写入 `Names`。
4. 直接字段天然比更深的嵌入字段优先。

这样比当前 map 覆盖更稳定，也比静默选一个字段更容易定位问题。

---

## 8. 错误模型

公共错误建议：

```go
var (
	ErrNotStruct      = errors.New("grx: type is not a struct")
	ErrFieldNotFound  = errors.New("grx: field not found")
	ErrAmbiguousField = errors.New("grx: ambiguous field")
	ErrUnexported     = errors.New("grx: field is unexported")
	ErrCannotSet      = errors.New("grx: field cannot be set")
	ErrNilValue       = errors.New("grx: nil value")
)

type FieldError struct {
	Op    string
	Type  reflect.Type
	Name  string
	Path  string
	Index []int
	Err   error
}
```

`FieldError` 实现 `Error()` 和 `Unwrap()`。调用方可以通过 `errors.Is(err, ErrAmbiguousField)` 判断类型，同时错误文本保留字段名、路径和结构体类型。

现有 `SetValue` 可以保留静默版本以兼容；新增 `SetValueStrict` 或 `Accessor.Set` 返回错误。

---

## 9. 性能目标与基准

新增基准应覆盖冷启动和热路径：

| Benchmark | 目标 |
| --- | --- |
| `BenchmarkMapperTypeMapCold` | 构建成本可见，用于比较不同字段规模。 |
| `BenchmarkMapperTypeMapWarm` | 缓存命中路径接近 map lookup，目标 0 alloc/op。 |
| `BenchmarkTraversalsByName` | 批量 10/100 字段匹配只分配返回切片。 |
| `BenchmarkTraversalsByNameFunc` | callback 版本目标 0 alloc/op。 |
| `BenchmarkFieldByIndexesReadOnly` | 不分配，不修改 nil pointer。 |
| `BenchmarkFieldByIndexesAlloc` | 只在首次 nil pointer/map 时分配。 |
| `BenchmarkBindMap` | 对比逐字段反射绑定，减少重复查找和 tag 解析。 |

性能目标不是追求 unsafe 极限，而是保证“同一类型解析一次，运行时不再重复解析 tag/字段树”。第一阶段验收重点是缓存命中路径和 traversal 预编译。

---

## 10. 兼容与迁移

### 10.1 保留现有 API

以下 API 保持可用：

- `NewFieldCache`
- `FieldCache.CacheStructFields`
- `FieldCache.GetStructField`
- `FieldCache.GetCachedStructFields`
- `FieldCache.Fields`
- `FieldCache.LookupFieldInfo`
- `FieldCache.LookupFieldByTag`
- `FieldCache.LookupMethod`
- `Fields`
- `LookupFieldInfo`
- `LookupFieldByTag`
- `Methods`

### 10.2 渐进式委托

第一步可以并行引入 `Mapper`，不改 `FieldCache` 行为。等新测试覆盖充足后，再让 `FieldCache` 内部委托默认 mapper。

`GetCachedStructFields` 暴露内部 map 的问题不应马上破坏兼容。建议：

- 保持返回 map，但改为返回拷贝。
- 在 README 中推荐新代码使用 `Mapper.TypeMap`。
- 后续大版本再考虑标记旧 API 为 deprecated。

---

## 11. 对上层包的价值

### ghttp

- 请求参数、header、query、path、body 绑定可以按 request 类型预编译字段 traversal。
- setup 阶段就能发现 tag 冲突和不可设置字段。
- handler 热路径避免重复 tag 解析。

### gsql / sql 相关能力

- rows columns 到 struct traversal 可按目标类型和列名列表缓存。
- scan 时只需按 traversal 取字段地址。
- 对嵌入字段和 db tag 的处理可与 HTTP/config 共享。

### config / map binding

- `map[string]any`、`map[string]string`、环境变量等可以共用 `Mapper`。
- 不需要每个包重复实现 tag option 解析和字段路径逻辑。

---

## 12. 分阶段交付

### 阶段 1：元数据底座

- 新增 `Mapper`、`StructMap`、`FieldInfo`、`TagOptions`。
- 实现 type cache、tag/name 解析、BFS 构图、冲突记录。
- 增加单元测试和基础 benchmark。

### 阶段 2：访问与批量 traversal

- 新增 `FieldByIndexesReadOnly`、`FieldByIndexesAlloc`、`Accessor`。
- 新增 `TraversalsByName`、`TraversalsByNameFunc`。
- 覆盖 nil pointer/map、不可设置字段、只读不分配。

### 阶段 3：轻量绑定

- 新增 `Binder`。
- 支持 map/string map 到 struct 的基础转换。
- 增加严格模式和错误聚合前先保证默认模式稳定。

### 阶段 4：兼容 API 迁移与 README

- `FieldCache` 内部逐步复用 mapper。
- `GetCachedStructFields` 返回拷贝。
- README 增加 mapper、traversal、binding 示例。

---

## 13. 验收标准

- `go test ./grx` 通过。
- 新增 benchmark 可运行：`go test ./grx -bench . -benchmem`。
- 新 API 在并发下缓存构建一次，读路径无 data race。
- 嵌入字段、tag option、冲突、未导出字段、nil pointer/map 都有单元测试。
- README 展示推荐 API，旧 API 保持兼容。
- 不新增外部依赖。

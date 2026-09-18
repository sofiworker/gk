# gai SDK 公开 API 与 Hook 生命周期重设计

状态：待评审设计，未实现。本文代码均为拟议 API，不能按当前 SDK 编译。目标是先确定调用者怎样使用，再决定内部如何拆分。2026-09-18。

本文调整 [Context 设计](2026-09-18-gai-context-design.md) 的组件组装与生命周期，不撤销完整历史、派生上下文、Sandbox 隔离视图等原则。已有 Runner/Runtime 代码是原型，不作为本设计必须兼容的公开 API。

## 1. 使用目标与显式计划

- [x] 核对现有 API、验证位置、环境绑定及压缩路径。
- [x] 给出单次调用、持续会话、Sandbox、Hook 四种使用方式。
- [x] 明确 Model/Client/Context 归属和各阶段不可绕过的检查。
- [x] 明确默认值、失败语义、资源生命周期与迁移步骤。
- [ ] 实现新接口、示例和生命周期测试；迁移后删除重复入口。

普通使用者只需要认识 Agent、输入和输出。持续交互才需要 Session；访问资源才需要 Sandbox；改变行为才需要 Hook。用户不需要为普通调用配置 Store、Checkpoint、ContextBuilder、ToolContext，也不需要再次选择同一个模型。

SDK 以 Go 调用为主，不要求 HTTP server、数据库、容器、外部审批服务或额外后台 goroutine。后端可以自行把 Agent 挂在 HTTP handler 或任务系统中。

## 2. 四种使用方式

以下片段中的 chatModel 是应用已构建的厂商无关 model.Model。厂商客户端只在模型创建处配置一次；工具实现和 Hook 是可信 Go 代码。

### 2.1 声明 Agent，直接运行

```go
a, err := agent.New(
    agent.WithModel(chatModel),
    agent.WithInstructions("你是开发助手"),
)
if err != nil {
    return err
}
result, err := a.Run(ctx, agent.Text("解释这个函数"))
if err != nil {
    return err
}
fmt.Println(result.Text())
```

New 固定一份不可变配置，可并发调用。未声明 ID/Version 时生成本地实例身份，便于追踪；持久化恢复和跨实例复用需要应用显式提供稳定身份版本，不能拿临时 ID 充当部署版本。

Run 创建仅本次调用存活的执行状态，返回 Result 中的消息、状态和标识。不会把上一次独立 Run 的内容自动放进下一次输入；也不要求为普通 Chat 创建 Sandbox。

### 2.2 连续会话

```go
s, err := a.NewSession(ctx)
if err != nil {
    return err
}
defer s.Close()

first, err := s.Run(ctx, agent.Text("分析这个模块"))
if err != nil {
    return err
}
second, err := s.Run(ctx, agent.Text("基于刚才的结论继续"))
if err != nil {
    return err
}
fmt.Println(first.Text(), second.Text())
```

session 包仍提供数据读写契约；这里的会话句柄放在 agent 包，持有固定的 Agent 引用、Session ID 和会话服务，不让底层 session 包反向依赖 agent。core.Session 是事实数据，不含 Run 方法或连接。

默认使用会话级内存 Store。第二次 Run 自动读取事实历史及已生效的上下文派生记录，不让调用方复制 Messages。默认同一 Session 只有一个活动 Turn，其他调用返回 ErrBusy；不同 Session 可并行。Session.Close 取消该句柄拥有的执行并释放自己创建的资源，不删除外部持久化历史；共享 Store/客户端由调用方关闭。

WithSessionStore 是高级依赖替换入口，通常在 Agent 构建时注入；OpenSession(ctx, id) 使用已有 Store 打开句柄，并核对 Agent 版本和环境可用性。创建 Session 与打开 Session 的行为不能混在一起隐式猜测。

### 2.3 使用 Sandbox 与工具

```go
box, err := memory.New()
if err != nil {
    return err
}
// 工具集只暴露经 Sandbox 中介的基础文件操作。
// The tool set exposes only basic file operations mediated by Sandbox.
a, err := agent.New(
    agent.WithModel(chatModel),
    agent.WithInstructions("按请求检查并修改文件"),
    agent.WithTools(builtin.Files()...),
    agent.WithHooks(hook.ToolPolicy(allowFileTools)),
)
if err != nil {
    return err
}
s, err := a.NewSession(ctx, agent.WithSandbox(box))
if err != nil {
    return err
}
defer s.Close()
result, err := s.Run(ctx, agent.Text("创建 note.txt"))
```

builtin.Files 是拟议便利函数，内部处理固定工具构建，不要求用户依次调用多个无配置的 NewX。allowFileTools 是应用的工具策略函数，返回允许/拒绝及理由。注册工具不自动授权；工具集、执行授权和资源权限分别生效。

外部传入的 box 是借用资源，Session.Close 不销毁它，生命周期由创建方管理。默认文件工具没有 Sandbox 时返回能力缺失，不偷偷读取宿主 cwd。需要自动创建环境时使用 WithSandboxFactory；SDK 提供内存工厂，一次 Session 一份，Session.Close 负责释放。普通 Chat 不会为工具之外的需求付出环境生命周期成本。

资源挂载、路径、读写策略及同步模式仍由 Sandbox 的可信管理入口配置。工具只得到本次调用的受限 View。缺少资源时可由 Hook/资源申请回调交给后端策略自动处理，或返回等待状态；不能在 SDK 内直接弹交互提示。没有审批处理器时不永久等待人员操作。

一次性 Run 也支持 WithSandbox(box) 执行选项：只绑定本次执行，不修改 Agent，也不改变已有 Session 的环境。会话运行中更换环境需要单独的受控操作，不允许任意请求 Hook 修改环境句柄。

### 2.4 请求前与响应后 Hook

```go
a, err := agent.New(
    agent.WithModel(chatModel),
    agent.WithHooks(
        hook.BeforeModel(func(ctx context.Context, e hook.ModelRequest) (model.Request, error) {
            req := e.Request // SDK 提供独立副本。
            req.Parameters.Temperature = model.Value(0.2)
            return req, nil
        }),
        hook.AfterModel(func(ctx context.Context, e hook.ModelResponse) (model.Response, error) {
            return normalizeResponse(e.Response), nil
        }),
        hook.ValidateOutput(checkBusinessOutput),
        hook.Observe(recordEvent),
    ),
)
```

Value 是拟议的指针便利函数，通过反射或显式类型创建参数指针。Hook 参数包含只读 RunID/TurnID/CallID、Purpose 和独立数据副本，不含 Store 管理对象、可变 Session、凭据或 Sandbox 管理器。注册后 Hook 实例可能并发使用，要求线程安全；每次运行局部状态通过有类型的调用数据保存，不写实例共享 map。

### 2.5 类型化输出：非泛型接口

拟议接口为 `output.JSONType(sample any) Spec` 和 `output.Bind(target any) Spec`。前者仅由类型生成 JSON Schema；后者额外绑定最终结果。`agent.Textf("生成一个虚构人物，按照 {} 返回", output.JSONType(Person{}))` 将 Schema 插入提示词，同时声明本次输出验证约束；不把样例字段值发送给模型。不提供泛型版本。

| 输入类型示例 | 约定 |
|---|---|
| `Person{}`、`&Person{}`、`(*Person)(nil)` | 顶层指针是类型占位，逐层剥离，生成相同的对象 Schema |
| `[]Person{}`、`[]Person(nil)` | 可变长度 JSON 数组；样例长度不作为约束 |
| `[2]Person{}`、`[0]Person{}` | JSON 数组，`minItems` 和 `maxItems` 均为 Go 数组长度 |
| `map[string]Person{}` | JSON 对象，`additionalProperties` 使用 Person 的 Schema |
| `map[string]any(nil)` | JSON 对象，值可为任意 JSON |
| `struct{}{}`、`(*struct{})(nil)` | 无字段的封闭对象：`properties: {}`、`additionalProperties: false`，只接受 `{}` |
| `any(Person{})` | 按动态类型 Person 处理，转成 any 不会丢失具体类型 |
| `nil`、`any(nil)` | 无运行时类型，请求前报错 |

`(*Person)(nil)` 是将 nil 转为具体指针类型的常见写法，并不是把空结构体强转成 Person。`struct{}{}` 无法表达未知结构体；若要表示任意对象，用 `map[string]any(nil)`。

顶层类型占位与内部可空值分开：根指针不自动允许 JSON null；结构体内部的指针、切片和 map 允许 null，`[]*Person` 的元素允许 null。根切片和 map 要求数组和对象。`omitempty` 控制属性是否必需，不等于允许 null；遵循 `encoding/json` 的字段名、忽略字段、嵌入字段选择及 `,string` 语义。普通结构体默认拒绝额外字段。递归类型通过 `$defs`/`$ref` 表达，不能无限展开。基本标量按 JSON 类型生成；非 string-keyed map、函数、channel、complex、unsafe pointer 以及无法可靠推导的自定义 JSON 编码类型请求前报错，后者须提供显式 Schema，不能静默猜测。

`Bind` 的最外层必须是非 nil 可写指针：`Bind(&person)`、`Bind(&slice)`、`Bind(&array)`、`Bind(&mapping)` 均合法；`var person *Person; Bind(&person)` 也合法，成功时分配内层对象。`Bind(Person{})`、`Bind(nil)`、`Bind((*Person)(nil))` 均非法。绑定空接口目标不能提供具体结构约束，应使用具体目标类型。

Spec 保存类型或目标及配置错误，由 Run 在发送模型请求前返回错误，不 panic。最终答案经过响应转换、Schema 与业务验证，并解码到全新临时值；全部成功且完成状态保存成功后才替换目标，不复用旧 map、slice 或嵌套指针，以保证失败不修改目标。工具调用的中间响应不触发绑定。同一目标不得由并发 Run 共享写入。

JSON 输出描述编译为内置验证和最终绑定阶段，不在 Agent 主循环添加 JSON 分支。后续 XML、文件、Markdown 通过相同输出描述抽象扩展各自提示词、验证和解码能力，不要求所有格式先转换成 JSON。

## 3. 对象划分

| 对象 | 状态与生命周期 | 负责什么 |
|---|---|---|
| agent.Config | 构建输入，复制后固定 | 指令、工具、Hook、模型依赖、可选基础设施 |
| agent.Agent | 可复用实例 | Run/NewSession 入口和已编译执行管线；不保存会话历史或共享活动 Sandbox |
| core.Agent 描述 | 可选持久化身份描述 | ID/版本/名称及可序列化行为信息；不强制序列化 Go 工具或客户端 |
| 会话句柄 | 一段持续交互 | 关联 Agent、Session ID、环境与状态服务 |
| core.Session/Turn | 不可随意改写的事实 | 输入、输出、调用与状态；不能被普通 Hook 直接操作 |
| Model Context | 单次模型调用 | 从历史构建的可见视图、来源与计量，不是第二份完整历史 |
| model.Model | 可复用模型绑定 | 逻辑身份、元信息、默认参数和独立 Client/计数能力的组合 |
| model.Client | 可复用执行依赖 | Generate 等中立调用契约；不持有 Session、Agent 或 Sandbox |
| Sandbox | 可选会话/运行资源 | 文件与网络中介、审计、快照和同步；不会因为摘要而回滚 |

此前“Agent 不持有模型”的约束针对行为描述和会话状态。构建后的 Agent 实例可以通过依赖注入持有 model.Model。模型默认值无需在 CreateSession 和每次 Run 中重复填写。模型切换通过受信执行选项选择完整 Model 绑定，同时重建能力、计量和上下文；不允许 BeforeModel 只改模型字符串绕过预算绑定。

Model.Info 仅是信息，Parameters 仅是生成参数，Client 仅是执行能力；Model 是便利组装对象，不把厂商 endpoint、key、wire model 塞进公共请求。

```go
// 自定义 Client 的高级组装草图；普通用户通常直接从厂商适配器取得 Model。
// Advanced composition for custom clients; typical users obtain Model from an adapter.
chatModel, err := model.New(info, client,
    model.WithParameters(defaults),
    model.WithCounter(counter),
)
```

真实厂商接入必须有独立适配器及测试，才能宣称对应厂商可用。ClientFunc 只便于接入自定义服务与测试，不是 SDK 缺少厂商适配的替代说明。

## 4. 简洁主线与固定边界

主线保留：建立执行 → 接收输入 → 构建上下文 → 模型调用 → 工具调用或返回结果 → 完成执行。

```text
Run/Session.Run
  ├─ BeforeRun → 输入基本检查 → 建立 Turn
  ├─ 循环
  │   ├─ BeforeContext → 构建候选视图 → AfterContext
  │   ├─ BeforeModel → 冻结请求 → 通用/能力验证 → 最终预算关口
  │   ├─ 保存调用意图和输入快照 → Client.Generate
  │   ├─ 记录原始归一化响应 → 基础结构检查 → AfterModel
  │   ├─ 检查最终响应 → 记录有效响应/转换事实
  │   └─ 如有工具：BeforeTool → 参数校验 → 授权关口
  │                  → 保存意图 → Execute → 记录实际结果
  │                  → AfterTool → 校验可见结果 → 记录投影视图
  └─ AfterRun → 保存最终状态 → 只读完成事件 → 返回
```

主循环不包含“如果摘要器存在”“如果 JSON Schema 存在”“如果要打日志”等业务分支。编译后的阶段管线处理这些扩展，固定边界统一处理结构不变量、资源权限和事实提交。

Hook 实现返回变更后的副本，由管线采用；禁止通过共享指针偷偷改下一阶段。请求冻结后没有可修改 Hook；观测只能读取副本。底层 HTTP 中间件只处理传输，不得静默增删业务消息或更换模型。凭据与协议默认字段由适配器处理。

## 5. Hook 契约

采用多个小接口，并提供函数适配器；不用实现包含十几个空方法的大接口。WithHooks 按注册顺序编译到固定阶段，不设置任意数值 priority。相同阶段按注册顺序执行；验证关口、授权关口、预算关口的位置不能靠注册顺序移动。

| Hook | 允许的作用 | 错误效果 |
|---|---|---|
| BeforeRun | 验证/转换本次业务输入 | 不调用模型/工具；不伪造完成 Turn |
| BeforeContext | 添加具有来源的材料引用和选择需求 | 停止构建，不直接修改历史 |
| AfterContext | 修改候选视图，返回有来源的 patch | 后续重新检查来源、配对和预算 |
| BeforeModel | 修改生成参数与候选输入；添加内容必须声明来源 | 冻结前停止，之后无请求发送 |
| AfterModel | 转换规范化响应、业务输出验证 | 保留原始调用事实并失败；工具不执行 |
| BeforeTool | 转换待执行参数、业务检查 | 不执行工具；之后仍需参数验证与授权 |
| ToolPolicy | 对最终参数与环境给出决定 | 拒绝/等待/允许，不修改参数 |
| AfterTool | 转换给模型看的工具结果 | 实际副作用与执行状态保持原样；记录转换失败并停止 |
| AfterRun | 处理最终业务结果，校验最终答案 | 记录 postprocessing_failed，不假装已执行动作未发生 |
| OnError | 只读观察结构化失败 | 不恢复、重放或清除原错误 |
| Observe | 只读进度/调用/完成事件 | 记录诊断，不把成功执行改成失败 |

ToolPolicy 是固定授权阶段的小接口，同样通过 WithHooks 注册。多个策略按拒绝优先合成：任一 deny 则拒绝，否则任一 pending 则等待，其余全部 allow 才允许。没有策略默认 deny。策略失败视为拒绝，不允许另一个策略的 allow 覆盖失败。

AfterModel 先于工具识别/分发完成。它不能伪造 Usage、RequestID、实际模型身份或 ToolExecution 状态；这些属于事实。允许转换工具参数时必须保留调用关联身份，并在 ToolPolicy 看到最终参数。删除/增加工具调用、改调用 ID 默认拒绝；需要替代生成或缓存响应的功能以后通过独立 typed outcome 设计，不从 AfterModel 暗中创造调用。

修改工具结果时保留 CallID、ExecutionID 和真实错误状态；可见错误说明可以脱敏，但不能把真实错误转换为成功标志。修改历史材料必须有 provenance；仅把另一份 []Message 塞回来不满足来源追踪要求。

传输失败或无有效响应时不运行 AfterModel，产生 OnError；模型正常返回但业务验证失败仍记录该模型调用和其用量。AfterTool 仅在进入 Execute 且回调返回后运行，可看到执行错误；授权拒绝只发对应事件，不冒充工具已经执行。

语义型 Hook 的 panic 转为带 Hook 身份的错误并中止，清理执行状态；工具已产生的副作用仍可能 unknown。观测 Hook 的 panic 作为诊断隔离，不改变运行结果。无异步 fire-and-forget 默认行为，回调必须遵守 context；不为超时创建无法终止的游离 goroutine。

## 6. 验证和授权的准确位置

1. New 时检查静态配置、工具名称冲突、选项组合和 Hook 配置。厂商模型可用性不能仅凭构建成功保证。
2. BeforeModel 完成后做唯一通用请求结构验证和目标模型能力验证；上下文预算依据最终候选请求重新计算。独立 model.Client 的调用者同样通过模型公共入口获得结构验证，不要求自己记住调用 Validate。
3. 解码后的归一化响应首先做最低结构检查；合法响应交给 AfterModel，转换后再验证。结构损坏的响应不进入业务 Hook；受限诊断事件仍可观察失败。
4. JSON/Schema/业务约束作为内置 ValidateOutput Hook，位于普通响应转换后。配置 Schema 验证却没有引擎时在 New 阶段失败。声明输出格式与本地验证是两件事。
5. BeforeTool 修改完成后，工具执行器校验最终参数并冻结；ToolPolicy 用同一份冻结参数和当前环境身份授权，授权后不再执行修改参数的 Hook。
6. Sandbox 每次文件/网络操作重新执行资源规则。ToolPolicy.allow 不能越过 Sandbox，也不能把只读挂载变为可写。

Hook 不是隔离边界。任意 Go Tool/Hook 若直接调用宿主 API，进程内内存 Sandbox 无法拦截；SDK 只保证经由能力接口的资源中介。工具策略授予的权限必须限定在这个真实边界内。

## 7. Context 和自动压缩放在哪里

Context Builder 是模型输入的基础组装能力，由默认配置创建，普通用户不用手写。Context 本身是每个调用的派生值。最终预算检查固定在请求冻结后，不能只在 BeforeModel 之前计数。

压缩是内置的专用 PrepareContext Hook：它收到冻结候选请求、计量和来源清单，返回带覆盖集合的 ContextChange。主循环不认识摘要模型、滚动摘要或压缩算法。该 Hook 的执行器提供受限的模型调用/原子提交接口，不把完整 Store 管理权限交给普通用户 Hook。

当 BeforeModel 扩大输入导致超限，最后预算关口调用 PrepareContext 管线：压缩后重新验证和计量，但不再次执行同一轮 BeforeModel，否则 Hook 每次追加材料可能无限增长。Hook 来源添加的内容默认 required；需要压缩它时必须由 Hook 显式声明允许省略/摘要。参数如输出预留不能在压缩阶段偷偷降低。

常用配置压缩为 `hook.AutoCompact(policy)`；默认使用 Agent 已绑定的 Model 信息和计数器，同一模型可以用于摘要，无须手工构造第二个 Builder。需要专门摘要模型才传 WithSummaryModel。未配置可靠预算能力时，启用 AutoCompact 在 New 阶段拒绝；普通 Chat 允许仅有 Client，但标记预算未知，不声称自动窗口保护。

摘要调用 Purpose=compaction，与 generation 共用基础模型调用及记录边界。观测默认看到全部 Purpose；普通 BeforeModel/AfterModel 默认仅针对 generation，显式选择 Purpose 才覆盖摘要；AutoCompact 永远不处理 compaction，防止递归。每次摘要调用占用独立预算并计入总成本，摘要后的恢复不会重放工具。

默认不启用额外付费的摘要请求；应用显式启用一次后可在整个会话自动运行，无须人员批准。压缩成功更新派生视图，Session 原文不变，Sandbox 无变化。后续显式 Session.Compact 与模型压缩请求复用同一 PrepareContext 能力，不另建执行循环。

## 8. 保存、重试和观测

必须修正之前“存储走 Hook”的笼统说法：核心事实 Store 是执行服务的依赖，具有提交成功才可继续的语义；默认内存实现自动配置。数据库持久化替换 Store。额外日志、外部投影和指标走 Observe，失败不等于事实事务失败。

一个模型调用涉及原始规范响应、Hook 转换及有效可见响应；保存原始事实和转换引用，避免 AfterModel 覆盖“模型到底返回了什么”。Turn.Messages 记录最终纳入对话的内容，ModelCall 关联原始产物引用及有效 MessageID；不把两份正文都冒充同一条历史消息。

请求意图与输入快照在发送前原子提交。Hook 错误发生在发送后时仍记账。工具实际结果先记事实，再转换可见投影；转换失败不能将工具标为未执行。Hook 管线身份、版本及关键变更摘要必须可追溯，涉及敏感值时提供脱敏策略，不能默认落凭据。

主线不自动重试整轮 Agent，也不让错误 Hook 重新调用 next。未来重试只作用于定义清晰的操作，通过独立策略配置，产生新的 AttemptID；Before/AfterModel 的次数与实际模型尝试一致。最终 context-limit 的单次安全恢复与工具重放分开。

不默认开放可任意调用 next 多次的 AroundTool/任意 middleware 链。这类入口会破坏授权、记账和副作用次数保证。需要缓存短路、替代响应或重试时逐项定义 typed outcome，而不是给所有 Hook 完整执行控制权。

## 9. 默认行为与资源归属

| 项目 | 默认行为 |
|---|---|
| Session / Store | 单次调用使用临时状态；NewSession 默认内存 Store |
| Sandbox | 无；显式实例或工厂才启用，绝不回退宿主文件系统 |
| 工具 | 无；注册仍需工具策略授权，Sandbox 再执行资源规则 |
| 模型 | 必须提供一次；Agent/Session 后续继承，不重复声明 |
| 参数 | Model 默认值，再由受信调用选项覆盖，最后由 Hook 修改并校验 |
| Context | 默认构建指令、历史和本轮输入；计量来自 Model 可选能力 |
| 自动压缩 | 显式安装 AutoCompact；依赖不足构建失败 |
| 业务校验 | 按需安装内置验证 Hook，结构校验始终执行 |
| 日志 | 不默认输出；Observe 可选 |
| 人工审批 | 不内置终端交互；policy 返回 pending 时由应用处理 |
| HTTP | 厂商适配器依赖标准传输接口；ghttp 通过组合适配 |

借用的 Client、Store、Sandbox 不由 Agent 任意关闭。SDK 工厂创建的会话资源由 Session.Close 释放。Session.Close 应幂等；存在执行时先取消并等待受限清理，具体等待时限需公开，不能在关闭时隐式同步宿主文件。

## 10. 包结构与旧代码处理

公开主入口保持 `gai/agent`，gai 根包继续文档入口；不为一个便利入口破坏现有依赖分层。

- `agent`：New、Run、会话句柄及用户选项。
- `hook`：类型化阶段契约、函数适配器和常用内置策略；契约只引用 core/model，不反向依赖 agent。
- `model`：Model、Client、Info、Parameters、Request/Response；厂商信息留 adapters。
- `context`：候选输入构建、来源/交互组及预算机制；压缩实现作为可组合 Hook 组件。
- `session`：事实 Store 及内存实现。
- `sandbox`、`tool`：能力与工具组件，保留已有隔离边界。
- 内部执行器：生命周期与阶段管线；不让用户直接配置 Runtime 再配置 Runner。

迁移顺序：

1. 先实现类型化 Hook 契约及阶段顺序测试；不同时保留两套互不一致的验证入口。
2. 添加 Agent 对外门面和默认组装，内部暂用既有执行能力，但请求、响应及工具边界必须已改成管线。
3. Session 句柄封装 Store 与环境生命周期，增加借用/自有资源测试；移除强制创建 Sandbox 的默认行为。
4. 压缩、业务验证和观测迁为内置 Hook，保存机制仍是核心依赖；去掉 Runner 的压缩特判。
5. 更新全部示例和 README，移除公开 Runner/Runtime 和嵌套 WithAgentOptions，显式记录破坏性变更。保留底层 model/tool/session 能力供高级使用。

保留现有测试中有价值的行为约束：工具配对、CAS、取消、失败不重放、Sandbox 审计与快照。不能因为现有类型已有测试就继续让它们主导 SDK 使用方式。

## 11. 交付验收

- 最小 Agent 示例包含 New 和 Run，不需要手写 Store、Builder、授权器或 Checkpoint；没有工具就不需要授权器。
- 文件助手只需额外声明工具、工具策略和 Sandbox；不要求理解 Session/Turn 内部记录。
- BeforeModel 和 AfterModel 覆盖每次真实 generation 调用；工具循环、错误及摘要 Purpose 的顺序测试通过。
- BeforeModel/BeforeTool 修改后的最终值经过验证、预算和授权；授权后没有参数修改路径。
- Hook 数据不跨并发调用串扰，panic/超时/取消不丢已发生事实，也不悄悄吞掉核心错误。
- Store 失败能阻断发送；Observe 失败不把已成功的副作用伪装成失败。
- AfterModel/AfterTool 转换前后的事实可追踪；失败校验不会触发工具执行。
- Context 压缩不是第二套主循环；新请求不会递归触发摘要；所有请求与摘要都可计量和追踪。
- 至少一个真实厂商适配器在独立测试中验证后，才宣称 SDK 可连接该厂商。当前模拟示例不能作为真实接入已完成的证明。

自检结论：本文给出拟议 API 和具体边界，不表示当前代码已支持 Hook。当前工作区仍是被评审否定的原型，不应标记为可交付 SDK。

# Tool

[中文](README.md)

Under development; not for production. `core.Agent.Tools` directly holds `[]Tool`. This package provides tool-set construction, model-visible descriptions, and execution dispatch, without a model loop.

## Responsibilities

| Object | Responsibility |
| --- | --- |
| `core.ToolDefinition` | Model-visible Name, Description, InputSchema; Version pins the implementation |
| `core.Tool` | Description, side-effect-free Validate, and Execute |
| `tool.Set` | Frozen selected tools, dispatched by model-visible Name |
| `core.ToolContext` | Per-call attribution and restricted file/network/resource-request capabilities |

Names use 1–64 ASCII letters, digits, underscores, or hyphens and must be unique in a Set. New copies and freezes descriptions and versions. Changes to Agent.Tools do not change an existing Set. Execution records use core.Binding with ID equal to the tool Name and the frozen Version for provenance, not implementation lookup.

## Construction and execution

```go
agent := core.Agent{ID: "reader", Tools: []core.Tool{readTool}}
set, err := tool.New(agent.Tools, tool.WithAuthorizer(authorize))
if err != nil {
    return err
}
call := core.ToolCall{
    ID: "model-call-1", Name: "read_file",
    Arguments: json.RawMessage(`{"path":"/project/file.txt"}`),
}
attribution := core.Call{
    ID: "execution-1", SessionID: "session-1", TurnID: "turn-1", ToolCallID: call.ID,
}
view := box.View(attribution)
execution, err := set.Execute(ctx, core.ToolContext{Call: attribution, Files: view}, call)
```

`readTool` implements core.Tool. A model adapter converts `set.Definitions()` into provider declarations. `authorize` is a trusted `func(context.Context, tool.Authorization) error`: nil permits execution and an error denies it. Policies can run automatically without human approval. Missing authorization defaults to denial. A trusted caller selects capabilities; Agent declarations grant no host or network access.

Order: lookup → JSON object/size checks → Tool.Validate → Authorizer → Tool.Execute. Validation and authorization receive separate argument copies. Duplicate JSON keys, multiple values, and excessive nesting are rejected. Authorization receives version, arguments, and attribution, without environment interfaces.

Execution returns the actual tool name and version, CallID, whether the callback started, status, timestamps, and ToolOutput. Go errors, panics, or cancellation after callback entry produce unknown status: effects may already have occurred. ToolOutput.Error produces failed status. Nothing is automatically retried or persisted. Runtime owns execution IDs, approval records, history, and ToolExecution/ToolResult association.

Repeating Execute with the same ToolCall is another attempt, not exactly-once execution. Sandbox file idempotency uses operation IDs; distinct writes require distinct stable IDs.

## Function adapter

`NewFunction[T](definition, validate, run)` adapts a typed Go callback into core.Tool. Both callbacks are required. Validate checks required fields, enums, ranges, and business rules without effects. Run receives context, ToolContext, and decoded T, returning multimodal ToolOutput.

An explicit valid JSON Schema object with type=object is required. This package neither infers schemas nor implements full JSON Schema validation. The adapter rejects unknown struct fields and uses json.Number for dynamic numbers. Validate must handle missing values, nulls, and schema constraints; tool authors maintain schema/type consistency.

## Lifecycle and limits

Sets support concurrent execution. Returned metadata copies cannot mutate a Set. Agent-held implementation objects remain trusted, behaviorally stable, concurrently reusable code; do not bind shared objects to a mutable Session environment. Different service configurations require separate instances.

Default argument size is 1 MiB and timeout is 30 seconds; WithLimits configures both. Deadlines cover validation, authorization, and execution but require cooperative cancellation. In-process Go callbacks cannot be forcibly killed. The dispatcher never abandons a background callback while claiming it has stopped.

MCP, dynamic toolsets, streaming tools, discovery, output-schema validation, and model Runtime are not implemented in this first version.

## Built-in file tools

[builtin](builtin/README.en.md) provides six basic file-tool constructors for Agent.Tools, operating through controlled file interfaces with limited command compatibility.

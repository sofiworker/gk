# Agent runner

[中文](README.md)

Under development; not for production. New(definition, client, options...) builds a reusable Runner owning a model.Client independently of the Agent definition.

Run receives explicit model selection, messages, and per-run ToolContext. Defaults allow 8 model rounds and 32 tool calls. WithLimits configures budgets; WithToolOptions configures authorization, denied by default.

Tools execute sequentially. Duplicate call IDs are rejected. Errors stop execution and return partial results without retries. The caller owns persistence, Session/Turn concurrency, approvals, and synchronization. Static Prompt.System is supported; unresolved Prompt.Sources are rejected. Streaming, fallback, durable recovery and automatic session storage are not implemented. Injected clients/tools must support concurrency and cancellation. Use session/turn-scoped capability views for multiple calls rather than a view pinned to another call ID.

Use [Runtime](../runtime/README.en.md) for session execution. Input.Capabilities creates per-call views; Checkpoint receives snapshots before external operations and after successful results. Checkpoint failures stop execution. Result.ModelRecords/ToolRecords carry IDs, times and failure states. Runtime records final failure states; tool result ExecutionID links execution facts, and ModelCall.FinishReason retains normalized completion reasons.

WithOutputValidator supplies structured output validation. Runner validates requests and responses and rejects schema output without a validator before calling the model. See the [new API examples](../examples/README.en.md); these interfaces await implementation.

WithContextBuilder injects per-call construction and measurement. Agent.ContextPolicy declares soft/hard input limits. See [Context](../context/README.en.md). Checkpoint passes input snapshots to Runtime for recording.

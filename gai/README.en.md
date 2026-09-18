# gai

[中文](README.md)

## SDK examples and redesign status

See [proposed examples](examples/README.en.md) for Agent, Session, Sandbox and hooks. **These APIs are design proposals and are not runnable yet.**

Legacy manual-composition examples have been removed. The examples now exclusively present the new API.

**Breaking change:** Runner rejects empty requests, undeclared tool calls and malformed responses. Schema output requires a configured validator before execution.


Under development; prohibited for production use. See [DEVELOPMENT.md](../DEVELOPMENT.md).

The `gai/core` package provides data structures and tool execution contracts; `gai/sandbox` provides an in-process memory environment. This is a fresh rewrite with no compatibility guarantees for previous APIs or storage formats.

- Session: metadata, turns, storage version, and timestamps.
- Turn: metadata, messages, model calls, tool executions with approvals, an optional summary, and status.
- Message: role, multimodal content, structured tool requests or results.
- ModelCall: requested and actual models, retry references, usage, and timing.

Models, agents, and environments use identifiers and execution-version references. Core types contain no vendor protocols or credentials. `TurnNotification` represents presentation notifications, not automatic model instructions. Call results reference message IDs without duplicating message bodies.

Structs do not enforce append-only storage, CAS, or authorization. runtime provides non-streaming execution; session/memory provides CAS and commit logs. Streaming and pagination remain unimplemented. Slices contain ordered supplied data; partial-loading semantics are not defined at this stage.

## Timestamps

All timestamps use `int64` Unix milliseconds, generated with `time.Now().UnixMilli()`. Optional timestamps use `*int64`; `nil` means unset, such as an unfinished execution or no expiration configured. Timestamps do not provide message ordering or CAS.

**Breaking change:** timestamp fields change from `time.Time` / `*time.Time` to `int64` / `*int64`. JSON values change from time strings to numbers or null.

**Breaking change:** `Turn.Summaries []Summary` becomes `Turn.Summary *Summary`; nil indicates no summary.

## Agent core

`core.Agent` is behavioral configuration containing ID, Version, Name, Prompt, Tools and ContextPolicy. Tools directly hold reusable implementations as `[]Tool`; execution capabilities are injected per call through ToolContext. An agent without tools serves ordinary chat.

**Breaking change:** `Agent.Model` is removed. Sessions provide model selection; an explicit selection for a turn overrides the session selection and is recorded in `Turn.Metadata.Model`. Model calls must be rejected when neither specifies a model. The runtime owns model clients; agents hold neither model selection nor model clients. runtime.Run resolves session or per-turn selections and invokes model.Client.

- **Breaking change**: removed Agent.Workspace, Agent.Scope, Agent.Sandbox, Scope, WorkspaceRef, and SandboxRef. Session/Turn/Approval now use EnvironmentBinding with ID, Revision, PolicyVersion, and Dir; references grant no access.
- **Breaking change**: Tool.Execute now explicitly receives ToolContext with per-call file, network, and resource-request capabilities; no global environment or mutable shared tool binding is required.
- Prompt holds static system instructions and ordered Skill/Resource sources. Selecting a system slot does not establish source trust.
- Sessions reference agents; turns record adopted bindings. These structs do not enforce permissions or execute conversations.

See [Runtime](runtime/README.en.md) for execution; independent adapters provide wire protocol mapping.

## In-memory Sandbox

See [Sandbox usage](sandbox/README.en.md) and the [design document](../docs/superpowers/specs/2026-09-17-gai-sandbox-design.md).

The built-in memory implementation provides caller-defined virtual directories, resource bindings, file content versions, audit, snapshots, immediate/explicit synchronization, and resource requests. The local adapter explicitly imports host directories and writes changes with conflict checks; httpgate provides controlled HTTP. Environment creation needs no host directory, database, or third-party sandbox and never automatically spills state to disk.

Runtime allocates a separate memory Sandbox per Session by default. Applications call Sync on explicit Turn/Session completion. Custom in-process Go tools remain trusted host code and are not isolated at the OS level. Core data structures alone enforce no policy; Sandbox interfaces enforce the mediated operations described above.

## Tool sets and execution

**Breaking change**: removed ToolRef and Registry. Agent.Tools is now []Tool. tool.New(agent.Tools, options...) directly builds a set with frozen descriptions/versions and unique names. Definitions exposes model descriptions; Execute validates, explicitly authorizes, and dispatches calls. Execution Binding.ID is the tool name.

NewFunction adapts typed callbacks; per-call ToolContext supplies environment capabilities. Execution defaults to denial without an authorizer and never automatically retries or persists history. See [tool documentation](tool/README.en.md) and ExampleNew.

## Agent and model execution

A basic [agent.Runner](agent/README.en.md) uses vendor-neutral [model.Client](model/README.en.md). [Protocol composition](adapters/modelhttp/README.en.md) is separate from [HTTP transport](transport/httptransport/README.en.md). Breaking change: the previous HTTP Codec/JSONCodec entry point is removed; callers must supply a protocol implementation. [runtime.Runtime](runtime/README.en.md) records Session/Turn and call facts, supports cancellation and allocates independent sandboxes. [session/memory](session/README.en.md) provides atomic commits and operation deduplication. Disk persistence, approval resumption and restart recovery remain unimplemented.

Per-call context construction, budget checks and atomic snapshot recording are available; see [Context](context/README.en.md). Automatic summarization can be explicitly enabled with a separate Compactor. Missing Builders use unknown-budget compatibility mode.

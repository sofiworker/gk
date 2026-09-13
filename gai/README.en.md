# gai

[中文](README.md)

Under development; prohibited for production use. See [DEVELOPMENT.md](../DEVELOPMENT.md).

The `gai/core` package provides data structures and execution interface contracts. This is a fresh rewrite with no compatibility guarantees for previous APIs or storage formats.

- Session: metadata, turns, storage version, and timestamps.
- Turn: metadata, messages, model calls, tool executions with approvals, an optional summary, and status.
- Message: role, multimodal content, structured tool requests or results.
- ModelCall: requested and actual models, retry references, usage, and timing.

Models, agents, workspaces, scopes, and sandboxes use identifiers and version references. Core types contain no vendor protocols or credentials. `TurnNotification` represents presentation notifications, not automatic model instructions. Call results reference message IDs without duplicating message bodies.

Structs do not enforce append-only storage, CAS, or authorization. Runtime execution, stores, streaming, pagination, and state-update logs are not implemented. Slices contain ordered supplied data; partial-loading semantics are not defined at this stage.

## Timestamps

All timestamps use `int64` Unix milliseconds, generated with `time.Now().UnixMilli()`. Optional timestamps use `*int64`; `nil` means unset, such as an unfinished execution or no expiration configured. Timestamps do not provide message ordering or CAS.

**Breaking change:** timestamp fields change from `time.Time` / `*time.Time` to `int64` / `*int64`. JSON values change from time strings to numbers or null.

**Breaking change:** `Turn.Summaries []Summary` becomes `Turn.Summary *Summary`; nil indicates no summary.

## Agent core

`core.Agent` is declarative configuration containing Workspace, Scope, Sandbox, Prompt, Model, and Tools. Tools use `ToolRef` references rather than executable instances. An agent without tools serves ordinary chat.

- Workspace and Sandbox use ID/version references. Missing values do not permit host resource access or unisolated execution.
- Scope describes exact resources and capability identifiers. Empty lists grant no capabilities; wildcards are not implicit.
- Prompt holds static system instructions and ordered Skill/Resource sources. Selecting a system slot does not establish source trust.
- Sessions reference agents; turns record adopted bindings. These structs do not enforce permissions or execute conversations.

Agent execution interfaces, model request protocols, and execution loops are not included.

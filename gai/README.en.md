# gai: AI Application Components

English | [中文](README.md)

> **Under development. Do not use in production.** See [development status](../DEVELOPMENT.md).

`gai` is intended to be a composable Go package family for model invocation, context construction, and agent execution.
Currently it contains only a package scaffold and capability plan, with no callable exported API. All packages below are planned; directories will be created as they are implemented.

## Planned capabilities

| Package | Responsibility |
| --- | --- |
| `model` | Model contracts, messages, streaming, tool calls, structured output, and usage |
| `providers` | Model service integrations and capability adaptation |
| `gateway` | Model routing, rate limits, fallback, and budgets |
| `prompt` | Templates, variables, dynamic context, and context budgets |
| `agent` | Agent configuration, role instructions, execution loops, and termination |
| `tool` | Registration, argument validation, authorization, and execution |
| `skill` | Metadata, discovery, and on-demand loading of instructions and resources |
| `session` | Conversation history, metadata, and storage contracts |
| `workflow` | Sequential and parallel execution, branching, and agent handoffs |
| `checkpoint` | Execution state, suspension, and resumption |
| `sandbox` | Isolated execution contracts, access policies, and backends |
| `adapters` | Explicit integration with other gk capabilities or external implementations |

## Design boundaries

- Keep the root package minimal and expose capabilities through optional subpackages. Prefer small interfaces and functional options.
- `gai` belongs to the capability layer. Dependencies within the family must remain acyclic. Core packages must not import other capability families; compose them through injected interfaces and `gai/adapters/<implementation>`.
- Reuse repository error and retry concepts through `gerr` and `gretry`. Tools with side effects must not be retried unconditionally.
- Model prompt construction separately from prompt injection defenses. Role instructions are not permission boundaries.
- Separate conversation history, cross-session memory, and execution checkpoints. Loading a skill does not grant tool permissions.
- Timeouts, goroutines, and ordinary subprocesses do not provide a security sandbox. Each execution backend must document and enforce its isolation guarantees.

See the [module dependency policy](../docs/superpowers/specs/2026-08-07-gk-module-dependency-policy.md).

## Implementation order

1. Model contracts and one provider, prompt construction, tool calls, a bounded agent loop, in-memory sessions, and execution events.
2. Persistent sessions, checkpoints, human approval, skills, and simple orchestration.
3. Model gateway governance and sandbox backends.

Tests and usage examples will accompany implemented behavior. API compatibility is not currently guaranteed.

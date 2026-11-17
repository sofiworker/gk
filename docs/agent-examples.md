# Agent examples

**English** | [简体中文](zh-CN/agent-examples.md) | [Documentation](README.md)

Executable examples are maintained as Go Example tests alongside implementation.
They use local fakes or in-memory components and do not require model API credentials.

| Example | Source | What it shows |
| --- | --- | --- |
| Session execution | [Runtime example](../agent/runtime/example_test.go) | Agent definition, fake model, memory Store, session creation and Run |
| Tools | [Tool example](../agent/tool/example_test.go) | Tool construction and explicit execution |
| Session behavior | [Runtime tests](../agent/runtime/runtime_test.go) | Recording, concurrency, cancellation and failure boundaries |

Run the examples from the repository root:

```sh
go test ./agent/runtime ./agent/tool -run Example -v
```

The broader SDK tests are `go test ./agent/...`.
[Runtime](agent-runtime.md), [tools](agent-tools.md), [context](agent-context.md) and
[Sandbox](agent-sandbox.md) describe the supported scope. Streaming, restart recovery
and approval suspension/resumption are not implemented.

## Proposed API examples

The original design samples remain in `agent/examples/`. They use `//go:build ignore`
and cannot currently run: `model.Model`, option-based `agent.New`, `hook` and the output
APIs shown there are not implemented. Models are passed in by the application;
the Sandbox sample borrows a caller-owned environment.

| Source | Proposed behavior |
| --- | --- |
| [agent.go](../agent/examples/agent.go) | Declare an Agent and run one request |
| [session.go](../agent/examples/session.go) | Continue a conversation with automatic history |
| [sandbox.go](../agent/examples/sandbox.go) | Bind a Sandbox, file tools and authorization |
| [hooks.go](../agent/examples/hooks.go) | Configure requests, transform responses and validate final answers |
| [structured_output.go](../agent/examples/structured_output.go) | Reflection-based output for structs, pointers, slices, arrays, maps and empty objects |

The proposed `JSONType(sample any) Spec` generates a schema without binding a response;
`Bind(target any) Spec` also decodes the validated final response into a non-nil writable
pointer. Both inspect runtime type information without sending sample field values.
String-keyed maps and typed nil values are supported; a nil interface has no runtime
type and fails before model dispatch. Failed binding leaves the target unchanged.

Once these APIs are implemented, remove the build exclusions and add compilation
and execution tests.

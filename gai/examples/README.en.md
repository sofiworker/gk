# Go examples

See [structured_output.go](structured_output.go) for the reflection-only output API. `JSONType(sample any)` generates a schema without binding a response; `Bind(target any)` additionally decodes the validated final response into a non-nil writable pointer. Both inspect only runtime type information; sample field values are never sent. Structs, pointers, slices, arrays, string-keyed maps and typed nil values are supported. A nil interface has no runtime type and fails before model dispatch. Failed binding leaves the target unchanged. These APIs remain unimplemented.

[中文](README.md)

- [agent.go](agent.go): declare an Agent and run one request.
- [session.go](session.go): continue a conversation with automatic history.
- [sandbox.go](sandbox.go): bind a Sandbox, file tools and authorization.
- [hooks.go](hooks.go): configure requests, transform responses and validate final answers.
- [structured_output.go](structured_output.go): structs, pointers, slices, arrays, maps, empty objects and failed-binding examples.

These files target the proposed API. `model.Model`, option-based `agent.New` and `hook` are not implemented yet. Files use `//go:build ignore` and cannot currently run. Application-configured models are passed as parameters; the Sandbox example borrows a caller-owned environment.

Once the APIs are implemented, remove the build exclusions and add compilation and execution tests.

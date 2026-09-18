# Agent runtime

[中文](README.md)

Pre-v1, not for production. Runtime connects Agent definitions, vendor-neutral model.Client, Session Store and Sandbox into a non-streaming execution path.

New(definition, client, store, options...) fixes a versioned Agent. CreateSession(ctx, metadata) selects a logical model and creates an independent memory sandbox by default. Run(ctx, sessionID, Input{Content: ..., Parameters: ...}) loads history, creates a Turn, executes the model/tool loop and records results. Applications can call it from HTTP handlers or background jobs. See [example_test.go](example_test.go) and [runtime_test.go](runtime_test.go).

Agent owns neither clients nor environments. Runtime owns execution; Session references an environment; Turn captures actual bindings. WithAgentOptions configures budgets and tool authorization. Tools are denied by default. Sandbox(environmentID) exposes default environments to trusted controllers for bindings, synchronization, snapshots and audit. Tools receive per-call views bound to session, turn and tool-call IDs. Paths are configurable. WithEnvironment resolves external environments and returns actual bindings and a capability factory; the trusted resolver owns authorization.

Running call records are committed before model/tool operations. Results and associated messages are committed atomically. One active Turn is allowed per Session, including across Runtime instances through Store CAS. Conflicts are never overwritten. Cancel(sessionID, turnID) cancels local execution. Each recording attempt after cancellation has a five-second timeout; custom stores must honor context.

Failures never automatically replay tools. A callback error may leave an unknown tool outcome. Unpaired tool requests prevent subsequent Run calls with ErrHistory. agent.ErrCheckpoint means storage failed: stored state may remain running and the returned Turn may include unsaved facts. Reconcile storage and effects explicitly before recovery. Persisted failures use fixed English messages; original errors are returned to callers.

Current scope excludes streaming, restart recovery, approval suspension/resumption, automatic model fallback, distributed cancellation and workflows. Memory storage has no crash durability. Custom stores must implement atomicity, operation deduplication and concurrency constraints. Session commits and Sandbox/host synchronization are separate operations; trusted controllers explicitly manage synchronization and rollback.

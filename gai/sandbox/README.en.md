# Sandbox

[中文](README.md)

Under development; not for production use. The built-in `memory` implementation keeps files, content versions, audit history, snapshots, and synchronization progress inside the host process. It starts no subprocesses and implicitly creates no host directories or network connections.

## Packages

| Package | Purpose |
| --- | --- |
| `sandbox` | Domain aliases for small file, network, audit, snapshot, and synchronization contracts defined in core |
| `sandbox/memory` | Memory environment, resource bindings, access checks, tool views, resource requests |
| `sandbox/local` | Explicit host import and root-confined synchronization with conflict checks |
| `sandbox/httpgate` | Explicit HTTP/HTTPS authorization, credential injection, and request limits |

No third-party sandbox or database dependencies are required. An environment initially contains only `/`. Internal memory files are writable by default; external resources and networking are unavailable until supplied. Directory names are caller-defined and imply no permissions or purpose. `WithAccess` configures internal access; each external binding has its own `Access`.

## Usage

```go
ctx := context.Background()
box, err := memory.New(memory.WithID("session-42"))
if err != nil {
    return err
}
if _, err = box.Mkdir(ctx, sandbox.Call{}, "/project-a"); err != nil {
    return err
}
view := box.View(sandbox.Call{
    Dir: "/project-a", SessionID: "42", TurnID: "1", ToolCallID: "tool-1",
})
result, err := view.WriteFile(ctx, sandbox.Call{ID: "write-1"}, "hello.txt", []byte("hello"))
```

`View` exposes file, HTTP, and resource-request methods without administrative methods. It fixes call attribution and working directory. Individual capability interfaces can be injected into `core.ToolContext`. A trusted runtime must select capabilities according to Agent tool declarations and application policy. Views cannot isolate arbitrary Go callbacks.

File operations work on complete files. Mkdir is nonrecursive; Remove is recursive but rejects binding roots and ancestors. Rename neither replaces an existing destination nor crosses bindings. Relative paths use `Call.Dir` or the configured default directory; absolute paths use the virtual root. Host cwd is never changed. Links, devices, writable handles, and native commands are unsupported.

A stable mutation `Call.ID` returns the original result for the same request and `ErrConflict` for a different request. `LocalApplied` and `SyncState` distinguish accepted memory changes from host synchronization. Idempotency records live until destruction; reaching the Operations limit rejects new mutations. Retry synchronization through `Sync`, not by rerunning Agent operations.

## Import and synchronization

`local.Import(ctx, hostDir, limits)` captures a fixed directory copy. `Provide` supplies its ID, caller-selected virtual path, access rules, and optional Target. The parent directory must already exist and the destination must not exist. Callers ensure a consistent host view during import. Symlinks, hardlinks, and special files are rejected.

- `SyncNone`: no Target; changes stay in memory.
- `SyncExplicit`: call `Sync(ctx, resourceID)`, including from application Turn/Session completion handlers.
- `SyncImmediate`: accept the memory change, then await synchronization; failures retain the memory change.

Imported content is the expected host baseline. Each batch freezes a revision and orders removals and creations by directory depth; renames become file changes. Partial failure retains the successful prefix; retries continue the frozen batch. Later changes cannot enter an older batch. `SyncTo` targets a retained snapshot without changing current state; `SyncStatus` returns current progress; `Operation` returns the original mutation result. Unknown external outcomes cannot be retried automatically: a trusted caller must reconcile the host and use `Rebase`. Rebase also resolves ordinary conflicts.

File and administrative operations within one environment are serialized, including during external synchronization. Audit and synchronization status remain queryable. Different environments can operate concurrently. Shared host destinations must reuse one `local.Target` coordinator and exclude uncoordinated host writers. Content comparison alone cannot prevent arbitrary concurrent host writes.

The local adapter currently supports Unix only and rejects other platforms. It uses `os.Root`, temporary files, and single-file replacement; it promises neither batch atomicity nor crash recovery nor POSIX metadata preservation. Created directories use 0700 and synchronized files use 0600. The owner closes the Target.

## Snapshots, audit, and limits

`Snapshot`, `Snapshots`, `Restore`, and `Release` manage file snapshots; `Diff` compares snapshots or a snapshot with current state. Restore creates a new revision, retains history, checks current resource identity and access, and cannot resurrect revoked resources. Active network requests and unresolved synchronization batches prevent restore. Restore also fails if the current default directory is absent from the snapshot.

Restore pauses automatic synchronization. Explicitly Sync compensating changes or ResumeSync to reenable future automatic triggers. Restore itself changes neither host files nor remote effects.

`Events(after, limit)` returns monotonically ordered events. Directory removal, rename, and restore include per-path content references; `Content` retrieves the bytes. Trusted controllers can `PruneEvents`; expired cursors return `ErrCursorExpired`. Collection retains content referenced by current state, snapshots, audit, or synchronization.

`WithLimits` bounds content/metadata budgets, individual file size, directory entries, events, snapshots, idempotency records, and resource requests. Bytes uses conservative logical accounting and reservations, not a hard Go heap or process RSS limit. Inputs, output copies, transient allocations, and trusted callbacks also consume memory. Exhausted audit capacity rejects operations without adding another rejection event; accepted file changes always have authoritative audit records. There is no automatic disk spill.

## Network and resource requests

`httpgate.New` permits no destinations by default. `WithRules` configures scheme, host, port, and methods; private addresses and redirects are denied by default. DNS results are checked before dialing the validated address directly. Environment proxies are ignored. Trusted rule Headers inject credentials. Every redirect is reauthorized and discards previous caller headers; the destination injects credentials under its own rule. Applications remain responsible for filtering sensitive response content.

`WithLimits` configures request/response sizes, concurrency, and timeout. Attach the transport through `memory.WithNetwork(client)`. With no external operations active, `SetNetwork` replaces it; nil revokes access. Audit records operation IDs, method, scheme/host/port, status, sizes, and timestamps, excluding paths, queries, bodies, headers, and raw transport errors. Custom HTTPSender implementations are trusted and must enforce their own policies. Unknown effects are never automatically replayed.

`RequestResource` creates a request only. Trusted controllers use `DecideResource` and then `FulfillResource`. Approval does not make resources available until provisioning succeeds; failed provisioning can be retried. Automatic policy can handle requests without UI or human interaction. This package sends no notifications and calls no models.

## Lifecycle and boundaries

`State` returns lifecycle and environment versions. `Close(ctx)` rejects new operations, cancels external work, and waits for completion. A timeout leaves the environment closing; Close can be called again. It never implicitly synchronizes. Audit, content, operation results, and synchronization status remain queryable after close. `Destroy(ctx, discard)` releases a closed environment; unsynchronized target changes require explicit discard.

Process exit loses the environment. Synced host files cannot reconstruct its full history. A model Runtime, automatic Turn/Session lifecycle, and native execution are not implemented; applications wire the explicit interfaces to their lifecycle. Arbitrary Go code can bypass these interfaces: this implementation governs mediated operations and is not an OS security boundary.

# Session storage

[中文](README.md)

Reader, Creator and Writer are small interfaces composed by Store. Create accepts an empty session with version zero and initializes its version. Commit submits the current state of one Turn using SessionID, ExpectedVersion and OperationID. Messages belong to Session.Turns; call records reference message IDs.

The [memory implementation](memory/store.go) atomically checks versions and active turns. Identical operation retries return their original version; changed contents conflict. Existing messages, terminal calls and completed turns cannot be rewritten. History retains commit records. All data crossing storage boundaries is copied. No crash durability, capacity limits or compaction is provided; repository development restrictions apply.

Store is a trusted runtime boundary, not tool authorization or wire-content validation. CAS does not provide exactly-once external effects or a transaction spanning model calls, tools and host synchronization.

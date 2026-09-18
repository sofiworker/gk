# Built-in file tools

[中文](README.md)

Basic implementations under development; not for production. NewListDir, NewReadFile, NewFindFiles, NewSearchText, NewWriteFile, and NewEditFile return `(core.Tool, error)`. Select tools in Agent.Tools and build a tool.New set with explicit authorization.

All operations use ToolContext.Files without retaining sessions or starting host commands.

Tools list children, read UTF-8 lines, recursively find paths, search literal/Go regexp text, create/overwrite files, and replace exact text. Find uses Go path.Match without **: patterns without slashes match filenames, others match full virtual paths. Edit requires a unique match unless replace_all is enabled.

Default result/line limit is 200, maximum 1000. Traversal is capped at 10000 entries, file/total search text at 8 MiB, and JSON output at 256 KiB. Count truncation is explicit; byte/scan overruns return ErrQuota. Reads reject binary data; searches skip it. Other access errors propagate. Mutation JSON retains LocalApplied/SyncState even if host synchronization fails.

Edit adapts Eino's replacement logic and uses memory.Sandbox/View.CompareAndWrite for atomic digest checks. Backends lacking this capability return ErrUnsupported. Optional expected_content_id is checked; otherwise the digest of the read content is still required when writing.

Double-star globbing, streaming, pagination cursors, full POSIX semantics, and command-line compatibility are not implemented.

## Third-party attribution

The replacement portion of edit.go is adapted from CloudWeGo Eino's `adk/filesystem/backend_inmemory.go`, Edit method, local revision `9d983b36`, Copyright 2024 CloudWeGo Authors, Apache-2.0. Source notices and modification attribution are retained. See [LICENSE-APACHE](LICENSE-APACHE). No third-party runtime dependency was added; other tools are local interface adaptations.

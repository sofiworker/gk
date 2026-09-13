# gai/id

[中文](README.md)

`id.NewV7()` returns canonical lowercase UUID v7 strings for sessions, turns, messages, and call records. Preserve model-supplied RequestID and ToolCallID values.

Adapted from google/uuid v1.6.0 version7.go under the included [BSD license](LICENSE). The implementation exposes only string generation, uses crypto/rand, and omits random pools and parsing. It supports the repository minimum Go 1.25.12 without requiring Go 1.27's uuid package.

Unix milliseconds, a submillisecond sequence, and a process-local mutex handle clock ties and rollback. Ordering refers to generation within the critical section, not concurrent return order, cross-process order, or monotonicity after restart. Logical sequence advancement may place timestamps slightly ahead of the wall clock. Message ordering and CAS still require sequences and versions.

Following Go 1.25 crypto/rand.Read semantics, the standard library terminates the process if secure randomness is unavailable; no insecure fallback is used.

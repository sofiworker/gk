# ghttp targeted k6 tests

[中文](README.md)

> This repository is pre-v1.0.0. These tools are development-only targeted tests and must not be treated as a production load-test plan, capacity result, or SLO.

This module combines a dedicated test server, k6 scenarios, a raw-socket adversarial probe, and resource snapshots. It covers contracts, routing, binding/codecs, errors and security, concurrent state, SSE/WebSocket, stability, and recovery. It targets protocol boundaries rather than a real application traffic model.

## Scenarios

| Scenario | Coverage |
| --- | --- |
| `contract_smoke` | Health, binding, output, Problem Details, and OpenAPI contracts |
| `routing_matrix` | Static, parameter, wildcard, group, and precedence routing |
| `binding_codec` | Mixed binding, content negotiation, and codec errors |
| `error_security` | Validation, authentication failure, panic recovery, and post-error health |
| `protocol` | Methods, HEAD, and routing wire behavior |
| `errors` | 404 and Problem Details baseline |
| `security` | Bearer matrix, secret isolation, and runtime-metric recovery |
| `state` | Namespace isolation, idempotency, ETag, and CRUD lifecycle |
| `sse` | Event-stream framing, events, and cancellation timing |
| `websocket` | Upgrade, echo, close, and recovery |
| `stability` | Mixed ramping/arrival-rate health, error, and state traffic |

## Metrics and thresholds

Shared metrics are `route_duration`, `route_success`, `schema_success`, `secret_leaks`, `auth_success`, `negotiation_success`, and `openapi_success`; streaming scenarios add dedicated recovery/cancellation metrics. Each scenario enables thresholds only for metrics it emits. Functional/security rates default to 100%, secret leaks must remain zero, and smoke HTTP p95 defaults below one second. These are development gates, not production performance commitments.

## Running

Go and k6 are required; Node.js is used for static JavaScript parsing. From the repository root:

```powershell
powershell -ExecutionPolicy Bypass -File ghttp/testdata/k6/scripts/check-js.ps1
powershell -ExecutionPolicy Bypass -File ghttp/testdata/k6/scripts/count-checks.ps1
powershell -ExecutionPolicy Bypass -File ghttp/testdata/k6/run.ps1 -Profile smoke -Scenario contract_smoke
```

Runner options include `-Profile`, `-Scenario`, `-OutputDir`, `-Addr`, `-BaseUrl`, `-StaticDir`, `-MaxBodyBytes`, `-Secret`, and `-ShutdownSeconds`. A supplied `BaseUrl` must exactly match this server process's `READY` URL. Shared k6 environment variables include `BASE_URL`, `PROFILE`, `VUS`, `MAX_VUS`, `ITERATIONS`, `RATE`, `DURATION`, `SOAK_DURATION`, and `SECRET`.

The runner owns the server lifecycle: it binds readiness to the process it started, runs the probes, records cleanup state, and attempts graceful shutdown before a forced fallback. Do not expose this development-only server to an untrusted network.

## Outputs and exit codes

The output directory contains server stdout/stderr, build logs, `resources.json`, `k6.log`, `k6-summary.json`, `rawprobe.json`, and `rawprobe.log`. The PowerShell runner writes `summary.json`; the Unix runner writes `run-summary.json`. Exit codes are: 2 arguments; 3 build/server; 4 k6; 5 rawprobe; 6 resources; 7 cleanup.

## Known limitations

- System-wide CPU percentage is explicitly unsupported on Windows; snapshots are not a process CPU profiler.
- Raw-probe results depend on OS TCP behavior, and malformed traffic may be rejected before reaching application code.
- k6 has limited visibility into SSE/WS and TCP half-close/truncation semantics; Go fixtures and rawprobe cover those boundaries.
- The runner executes one scenario per invocation; an outer orchestrator is required for the full matrix.
- Thresholds are development regression gates, not capacity, security-audit, or long-term reliability certification.

## Final validation record

All 11 scenarios were inspected with k6 v1.8.0 on Windows, and the reproducible counter found 162 unique check names. Smoke passed. The full stability run lasted about nine minutes and produced 58,173 requests and 290,868 checks with a 100% check pass rate, zero HTTP failures, HTTP duration p95 of 1.12ms, and p99 of 6.11ms. Rawprobe passed 15/15 cases and the runner summary reported `cleanup.forced=false`. System-wide CPU is intentionally reported as unsupported on Windows; a zero value must not be interpreted as measured idle CPU.

# ghttp targeted k6 tests

[中文](README.md)

> This repository is pre-v1.0.0. These tools are development-only targeted tests and must not be treated as a production load-test plan, capacity result, or SLO.

This module combines a dedicated test server, k6 scenarios, a raw-socket adversarial probe, and resource snapshots. It covers contracts, routing, binding/codecs, errors and security, concurrent state, stability, and recovery. It targets protocol boundaries rather than a real application traffic model.

## Scenarios

| Scenario | Coverage |
| --- | --- |
| `contract_smoke` | Health, binding, output, and Problem Details contracts |
| `routing_matrix` | Static, parameter, wildcard, group, and precedence routing |
| `binding_codec` | Mixed binding, content negotiation, and codec errors |
| `error_security` | Validation, authentication failure, panic recovery, and post-error health |
| `protocol` | Methods, HEAD, and routing wire behavior |
| `errors` | 404 and Problem Details baseline |
| `security` | Bearer matrix, secret isolation, and runtime-metric recovery |
| `state` | Namespace isolation, idempotency, ETag, and CRUD lifecycle |
| `stability` | Mixed ramping/arrival-rate health, error, and state traffic |

## Metrics and thresholds

Shared metrics are `route_duration`, `route_success`, `schema_success`, `secret_leaks`, `auth_success`, and `negotiation_success`. Each scenario enables thresholds only for metrics it emits. Functional/security rates default to 100%, secret leaks must remain zero, and smoke HTTP p95 defaults below one second. These are development gates, not production performance commitments.

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
- The runner executes one scenario per invocation; an outer orchestrator is required for the full matrix.
- Thresholds are development regression gates, not capacity, security-audit, or long-term reliability certification.

## Final validation record

All 9 scenarios passed smoke with k6 v1.8.0 on Linux. The full 300s stability soak produced 37,639 requests and 188,198 checks with a 100% check pass rate, zero HTTP failures, HTTP duration avg 0.41ms, p95 0.64ms, p99 1.05ms. Goroutine and heap growth remained bounded (≤50 / ≤64MB). Rawprobe passed 15/15 cases and the runner summary reported `cleanup_failed=false`, `exit_code=0`.
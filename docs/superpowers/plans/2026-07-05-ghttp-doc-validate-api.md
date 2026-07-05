# ghttp Doc and Validate API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace OpenAPI-specific route builder methods with a single `Doc(...DocOption)` entry, add route-level validation controls, and make `Produces` support multiple response media types.

**Architecture:** Keep runtime concerns on `RouteBuilder` and move documentation concerns into a small `RouteDoc` value configured by options. OpenAPI generation should infer request parameters, request body, and successful response schema from `Route[Req, Resp]` by default. Response encoding should negotiate within the route/group/server `Produces` list and fall back to the first configured media type.

**Tech Stack:** Go, `net/http`, existing ghttp codec manager, existing reflection schema helpers.

---

### Task 1: Add Failing Tests for New Public Semantics

**Files:**
- Modify: `ghttp/openapi_test.go`
- Modify: `ghttp/builder_test.go`

- [ ] Write tests for `Doc(Summary(...), Description(...), Tags(...), Success(...), Errors(...))`.
- [ ] Write tests proving OpenAPI auto-discovers `path/query/header/cookie` parameters and `Body` from `Req`.
- [ ] Write tests proving OpenAPI auto-documents success response schema from `Resp`.
- [ ] Write tests for `WithProduces(MIMEJSON, MIMEXML)` and route `Produces(MIMEJSON, MIMEXML)` Accept negotiation.
- [ ] Write tests for `Validate(fn)`, `Validate(fn, ValidationError(...))`, and `SkipValidation()`.
- [ ] Run targeted tests and confirm they fail for missing new API.

### Task 2: Implement Doc Options and OpenAPI Inference

**Files:**
- Create: `ghttp/doc.go`
- Modify: `ghttp/builder.go`
- Modify: `ghttp/server.go`
- Modify: `ghttp/group.go`
- Modify: `ghttp/openapi.go`

- [ ] Add `RouteDoc`, `DocOption`, `DocError`, `DocResponse`, and option helpers.
- [ ] Replace builder fields `doc/tags/operationID/reqType/pathType/queryType/responses` with `RouteDoc`.
- [ ] Remove old builder methods `Reads`, `PathSchema`, `QuerySchema`, `Responds`, `Tags`, and `OperationID`.
- [ ] Register OpenAPI metadata with `Req` and `Resp` reflect types plus `RouteDoc`.
- [ ] Infer request parameters and request body from `Req`.
- [ ] Infer default success response from `Resp` and doc success options.
- [ ] Add business-error doc responses from `Errors(...)`.
- [ ] Run OpenAPI tests until green.

### Task 3: Implement Multi-Produces Negotiation

**Files:**
- Modify: `ghttp/config.go`
- Modify: `ghttp/server.go`
- Modify: `ghttp/group.go`
- Modify: `ghttp/builder.go`
- Modify: `ghttp/openapi.go`

- [ ] Change server/group/route produces storage from single string to `[]string`.
- [ ] Change `WithProduces`, `Group.Produces`, and `RouteBuilder.Produces` to variadic.
- [ ] Resolve all declared content types during registration for setup-time validation.
- [ ] Select response codec by `Accept` only within the configured produces list.
- [ ] Fall back to the first configured produces when no Accept match exists.
- [ ] Use the same selection for framework error responses when possible.
- [ ] Run builder/runtime tests until green.

### Task 4: Implement Route-Level Validation Controls

**Files:**
- Modify: `ghttp/builder.go`
- Create or modify: `ghttp/validate.go`

- [ ] Add `Validate(fn ValidateFunc[Req], opts ...ValidateOption)` to route builder.
- [ ] Add `SkipValidation()` to route builder.
- [ ] Run route validator after parsing and before global validator.
- [ ] Map route validator errors to `422` by default.
- [ ] Allow `ValidationError(err)` to replace the returned validation error while preserving the original cause.
- [ ] Ensure `SkipValidation()` disables global validation but not explicit route validation.
- [ ] Run validation tests until green.

### Task 5: Remove Old API Call Sites and Refresh Docs

**Files:**
- Modify: `ghttp/*_test.go`
- Modify: `example/ghttp_usage/*`
- Modify: `example/server_review*/*`
- Modify: `ghttp/README.md`

- [ ] Replace old doc builder calls with `Doc(...)` options or remove redundant docs.
- [ ] Replace old `PathSchema` and `QuerySchema` tests with tagged request structs.
- [ ] Remove or update comments that describe `Responds` as an available API.
- [ ] Update README route builder table and examples.
- [ ] Run `go test ./ghttp -count=1`.
- [ ] Run affected example tests.
- [ ] Run broader validation if compile surface changed outside `ghttp`.

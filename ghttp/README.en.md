# ghttp

English | [中文](README.md)

> Under development. Do not use directly for production development.
>
> ghttp is pre-v1.0.0. Its public API, defaults, and performance implementation are not frozen. This breaking server rewrite removes `Route[Req, Resp]`, `RouteBuilder`, and their chained terminals.

ghttp provides HTTP client and server functionality on top of the standard `net/http` package. The server is centered on immutable `Operation` values: method, path, input, output, documentation, and business logic are compiled before being mounted on a `Server` or `Group`. Request dispatch does not scan struct tags or pass through the old `RouteBuilder`.

## Core Features

- Reusable, copy-on-write `Operation` endpoint descriptions.
- Explicit `Input[T]` contracts for path, query, header, cookie, and body values.
- Explicit `Output[T]` contracts for status, Content-Type, headers, serialization, and OpenAPI schemas.
- Registration-time validation of methods, paths, metadata, duplicate parameters, bodies, and outputs.
- A frozen method-aware route tree supporting static, `{param}`, and `{path...}` segments.
- OpenAPI 3.1 generated directly from input and output contracts.
- Direct calls and a direct-path fast path for common JSON endpoints.
- One error, validation, body-limit, envelope, and content-negotiation pipeline.
- Native HTTP handlers, typed HTTP functions, WebSocket, SSE, and safe static files.

## Installation

```bash
go get github.com/sofiworker/gk
```

```go
import "github.com/sofiworker/gk/ghttp"
```

The module currently requires Go 1.25.0 or later. Files using Go 1.27 generic methods are selected automatically by toolchain version.

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/sofiworker/gk/ghttp"
)

type LookupUser struct {
    ID     int64
    Locale string
}

type User struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

func main() {
    input := ghttp.MapInputs(
        ghttp.PathInt64("id", ghttp.Minimum(1)),
        ghttp.QueryString("locale", ghttp.AllowedValues("en-US", "zh-CN")),
        func(id int64, locale string) LookupUser {
            return LookupUser{ID: id, Locale: locale}
        },
    )

    getUser := ghttp.Handle(
        ghttp.Get("/users/{id}"),
        input,
        ghttp.JSONOutput[User](),
        func(ctx context.Context, lookup LookupUser) (User, error) {
            return User{ID: lookup.ID, Name: lookup.Locale}, nil
        },
    ).Doc(
        ghttp.Summary("Get a user"),
        ghttp.Tags("users"),
        ghttp.OperationID("getUser"),
    )

    server := ghttp.New(ghttp.WithOpenAPI("users", "0.1.0"))
    server.MustMount(getUser)
    log.Fatal(server.Run(":8080"))
}
```

`GET /users/7?locale=en-US` returns:

```json
{"id":7,"name":"en-US"}
```

## Operation Model

`EndpointBuilder` stores only an HTTP method and path. `Handle` compiles it with an input, output, and handler:

```go
operation := ghttp.Handle(
    ghttp.Post("/users"),
    ghttp.JSONBody[CreateUser](),
    ghttp.WithResponseHeader(
        "X-Contract", "create-user",
        ghttp.WithStatus(http.StatusCreated, ghttp.JSONOutput[User]()),
    ),
    createUser,
)
```

Built-in starts are `Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, `Options`, `Connect`, and `Trace`. Use `Endpoint` for a custom method:

```go
ghttp.Endpoint("PURGE", "/cache")
```

All Operation modifiers return copies:

| Method | Purpose |
|---|---|
| `Doc(options...)` | Add OpenAPI summary, tags, operationId, and related metadata. |
| `WithMiddleware(middlewares...)` | Add route-level standard `net/http` middleware. |
| `WithMaxBodyBytes(n)` | Override the body limit; `n <= 0` disables it for this Operation. |
| `WithErrorWriter(writer)` | Override the route-level error writer. |
| `WithProblemDetails()` | Use RFC 9457 `application/problem+json`. |
| `WithoutServerValidation()` | Skip the server Validator but retain descriptor validation. |
| `WithWebSocketOriginCheck(check)` | Override the WebSocket Origin check. |

`Method()` and `Path()` inspect the description. `Mount` returns registration errors; `MustMount` panics for programmer configuration errors:

```go
server.MustMount(operationA, operationB)

api := server.Group("/api", authMiddleware)
api.MustMount(operationA.Doc(ghttp.Tags("api")))
```

The same Operation may be mounted on multiple Servers or Groups. Prefixes, middleware, and server configuration are never written back into it.

## Before and After Go 1.27

Both syntaxes share `compileOperation`, all descriptors, and one execution pipeline.

Before Go 1.27, use the package-level generic function:

```go
operation := ghttp.Handle(
    ghttp.Get("/users/{id}"),
    ghttp.PathInt64("id"),
    ghttp.JSONOutput[User](),
    getUser,
)
```

Go 1.27 adds inferred generic methods:

```go
operation := ghttp.Get("/users/{id}").Handle(
    ghttp.PathInt64("id"),
    ghttp.JSONOutput[User](),
    getUser,
)
```

The package-level `Handle` remains available on Go 1.27 for source migration. Go 1.27-only files require the corresponding `gofmt`; older formatters cannot parse generic methods.

## Input Contracts

### Built-ins

| Constructor | Result | Behavior |
|---|---|---|
| `NoInput()` | `Input[EmptyInput]` | Reads no request data. |
| `PathString/PathInt64/PathBool/PathFloat64` | Scalars | Required path parameters. |
| `PathRemainder` | `string` | A `{name...}` catch-all parameter. |
| `QueryString/QueryInt/QueryBool/QueryFloat64` | Scalars | Required query parameters. |
| `QueryIntDefault` | `int` | Uses a default when absent. |
| `QueryStringDefault` | `string` | Uses a default when absent. |
| `QueryBoolDefault` | `bool` | Uses a default when absent. |
| `QueryFloat64Default` | `float64` | Uses a default when absent. |
| `HeaderStringDefault` | `string` | Uses a default when absent. |
| `CookieStringDefault` | `string` | Uses a default when absent. |
| `QueryStrings` | `[]string` | Repeated query values. |
| `HeaderString` | `string` | A required request header. |
| `CookieString` | `string` | A required Cookie. |
| `JSONBody[T]` | `T` | Required JSON body with optional validators. |
| `FormBody` | `url.Values` | URL-encoded body. |
| `MultipartFile` | `*FileHeader` | Multipart file field. |
| `HTTPRequest` | `*http.Request` | Input-side low-level escape hatch. |
| `ValidatedInput[T]` | `T` | Wraps any input contract with endpoint-level validation. |

Numeric parameters accept only `NumberConstraint` values from `Minimum` and `Maximum`. String parameters accept only `StringConstraint` values from `AllowedValues`. Invalid schema/runtime combinations therefore fail at compile time.

### Composition

```go
type Search struct {
    Tenant string
    Page   int
    Tags   []string
}

input := ghttp.MapInputs3(
    ghttp.HeaderString("X-Tenant"),
    ghttp.QueryIntDefault("page", 1, ghttp.Minimum(1)),
    ghttp.QueryStrings("tag"),
    func(tenant string, page int, tags []string) Search {
        return Search{Tenant: tenant, Page: page, Tags: tags}
    },
)
```

`CombineInputs`/`CombineInputs3` return `InputPair`/`InputTriple`; `MapInputs`/`MapInputs3`/`MapInputs4`/`MapInputs5` construct a business type directly (the 3/4/5-way versions are flat implementations without intermediate Pair nesting). Composed inputs share one request state, so query, body, form, and multipart data are parsed once.

Use `InputFunc` for arbitrary input. Add OpenAPI metadata with `InputFuncWithMetadata`:

```go
input := ghttp.InputFuncWithMetadata(
    func(request ghttp.RequestView) (Tenant, error) {
        return Tenant{ID: request.Header().Get("X-Tenant")}, nil
    },
    ghttp.InputMetadata{Parameters: []ghttp.InputParameter{{
        Name: "X-Tenant", Location: ghttp.ParameterLocationHeader,
        Required: true, Schema: map[string]any{"type": "string"},
    }}},
)
```

`RequestView` exposes `Context`, `HTTPRequest`, `Path`, `Query`, `Header`, `Cookie`, and `ClientIP`. It is valid only while constructing the input; returned query/header values must not be mutated.

Registration rejects duplicate or empty parameters, unsupported locations, path parameters absent from the route, multiple independent bodies, nil constructors, and nil mappers.

## Output Contracts

| Constructor | Output |
|---|---|
| `JSONOutput[T]` | JSON 200, serialized before committing the status. |
| `CodecOutput[T]` | Negotiates through Server-registered Codecs and `Accept`. |
| `TextOutput` | `text/plain` 200. |
| `XMLOutput[T]` | XML 200, serialized before commit. |
| `HTMLOutput[T]` | `html/template`, executed before commit. |
| `BytesOutput` | `[]byte` with an explicit Content-Type. |
| `NoContentOutput[T]` | 204 with no body. |
| `RedirectOutput` | The handler returns `RedirectResponse{Location: ...}`. |
| `DownloadOutput` | Byte response with an attachment filename. |
| `StreamOutput` | The handler returns `func(io.Writer) error`. |
| `SSEOutput` | The handler returns `func(*SSEWriter) error`. |
| `FileOutput` | Sends an `io.ReadSeeker` from its beginning. |
| `OutputFunc` | Custom status, Content-Type, and writer. |

Output wrappers:

- `WithStatus(status, output)` overrides the success status.
- `WithResponseHeader(name, value, output)` adds a fixed response header.
- `WithResponseCookie(cookie, output)` adds a response Cookie.
- `WithOutputSchema(schema, output)` overrides the success schema.
- `WithDocumentedResponses(responses, output)` declares additional OpenAPI responses.

Invalid status codes, nil templates, and nil custom writers fail at Mount. JSON/XML/HTML preparation failures, file seek failures, and SSE writer capability failures occur before response commit and still enter the unified error pipeline.

## Convenience Operations

```go
server.MustMount(
    ghttp.GetJSON("/users/{id}", ghttp.PathInt64("id"), getUser),
    ghttp.PostJSON("/users", ghttp.JSONBody[CreateUser](), createUser),
    ghttp.CreatedJSON("/users", ghttp.JSONBody[CreateUser](), createUser),
    ghttp.GetText("/health", ghttp.NoInput(), health),
)
```

`PutJSON`, `PatchJSON`, `DeleteJSON`, and corresponding Text forms are also available. A JSON operation with one path input uses direct value compilation instead of constructing a general parameter container.

## Native HTTP, WebSocket, SSE, and Static Files

```go
raw := ghttp.RawOperation(http.MethodGet, "/metrics", metricsHandler)

httpFunc := ghttp.HTTPFuncOperation(
    http.MethodGet, "/raw",
    func(w http.ResponseWriter, r *http.Request) error { return nil },
)

typedHTTP := ghttp.HandleHTTP(
    ghttp.Get("/files/{name}"),
    ghttp.PathString("name"),
    func(w http.ResponseWriter, r *http.Request, name string) error { return nil },
)
```

Go 1.27 adds `EndpointBuilder.HandleHTTP` as an inferred method.

WebSocket operations reuse ghttp connection, Origin, subprotocol, buffer, logger, and keepalive behavior:

```go
ws := ghttp.WebSocketOperation(
    "/chat/{room}",
    ghttp.PathString("room"),
    func(ctx context.Context, room string, conn *ghttp.WebSocketConn) error {
        return nil
    },
)
```

SSE is a regular output contract:

```go
events := ghttp.Handle(
    ghttp.Get("/events"),
    ghttp.NoInput(),
    ghttp.SSEOutput(),
    func(ctx context.Context, _ ghttp.EmptyInput) (func(*ghttp.SSEWriter) error, error) {
        return func(stream *ghttp.SSEWriter) error {
            return stream.WriteJSON("tick", map[string]any{"ready": true})
        }, nil
    },
)
```

Static files:

```go
server.MustMount(
    ghttp.StaticDirectory("/assets", "./public"),
    ghttp.StaticFileSystem("/embedded", http.FS(assets)),
    ghttp.StaticFile("/favicon.ico", "./favicon.ico"),
)
```

`StaticDirectory` uses `NewSafeFS`, rejecting traversal, backslashes, and symlink escapes.

## Errors, Validation, and Body Limits

Returning an `*HTTPError` preserves its status:

```go
return User{}, ghttp.NotFound("user not found")
```

The default error body is `{code,message}`. Server-level `WithProblemDetails()` or Operation-level `WithProblemDetails()` switches to RFC 9457. `WithErrorWriter` installs composable custom writers.

The server Validator is disabled by default:

```go
server := ghttp.New(ghttp.WithValidator(ghttp.NewDefaultValidator()))
```

Operation invokes it after input construction and returns 422 on failure. `WithoutServerValidation` skips only the server Validator; `JSONBody` validators and `InputFunc` validation still run.

`WithMaxBodyBytes` sets the server default and `operation.WithMaxBodyBytes` overrides one route. JSON, URL-encoded, multipart, and `RawBody` share request body state and one limit. Exceeding it returns 413.

Content negotiation is strict by default:

- A body Content-Type not accepted by the input contract returns 415.
- An `Accept` header excluding the output type returns 406.
- Empty `Accept` and `*/*` accept the output type.

## OpenAPI 3.1

```go
server := ghttp.New(ghttp.WithOpenAPI("users", "0.1.0"))
server.MustMount(operation)

document, err := server.OpenAPI()
```

`/openapi.json` is registered by default. Operation registration freezes:

- path/query/header/cookie parameters and required, format, minimum, maximum, and enum constraints;
- JSON, form, and multipart request schemas;
- success status, Content-Type, response headers, Cookies, and body schema;
- additional responses, Problem Details schemas, and WebSocket/SSE extensions;
- summary, description, tags, operationId, deprecation, sunset, and external docs.

Missing summaries and operation IDs are inferred. For example, `GET /users/{id}` becomes `get_users_by_id`. An explicit `OperationID` always wins.

## Server and Middleware

`Server` implements `http.Handler`. Routes freeze before the first request; later registration returns or panics with `ErrServerFrozen`.

```go
server := ghttp.New(
    ghttp.WithAddress(":8080"),
    ghttp.WithReadHeaderTimeout(5*time.Second),
    ghttp.WithIdleTimeout(60*time.Second),
)

server.Use(ghttp.RequestID(), ghttp.Recoverer())
server.SkipUse(authMiddleware, http.MethodGet, "/health")

go server.Run()
defer server.Shutdown(context.Background())
```

`Server.Use`, `Group`, and `Operation.WithMiddleware` all use `func(http.Handler) http.Handler`. Execution order is server, group, then Operation from outermost to innermost.

Typed keys ride the standard request context, readable and writable by plain net/http middleware:

```go
var requestID = ghttp.NewKey[string]("request-id") // package-level; the type is fixed at compile time

ctx = requestID.Set(ctx, "abc123")
id, ok := requestID.Get(ctx)
```

## Client

The client API remains independent:

```go
client := ghttp.NewClient()

response, err := client.R().
    SetHeader("Authorization", "Bearer token").
    SetQueryParam("lang", "en-US").
    Get("/users/7")

typed, err := ghttp.GET[GetUserRequest, User](client, "/users/7", nil)
```

It supports retries, before/after hooks, authentication, Cookies, request timeouts, streaming responses, error-model binding, and custom `http.Client`/Transport values. Go 1.27 also provides generic client methods.

## Breaking Changes

`Route[Req, Resp](target).GET(path).To(...)`, the Go 1.27 `server.GET(path).To(...)` form, `RouteOption`, and all former terminals are removed. There is no deprecated shim or test-only compatibility entry point.

Historical breaking changes in this rewrite include:

- `Operation` plus `Mount/MustMount` is the only registration model.
- Explicit `Input[T]` descriptors replace implicit endpoint binding; use `StructInput[T]` for struct binding.
- Explicit `Output[T]` contracts replace inferred output behavior; use `CodecOutput[T]` for multiple formats.
- Response wrappers are named `WithResponseHeader` and `WithResponseCookie`.
- Redirect locations come from `RedirectResponse.Location`.
- Constraints are split into `NumberConstraint` and `StringConstraint`.
- Inferred operation IDs use normalized `method_resource_by_parameter` names.

### StructInput Removal (Performance Convergence)

`StructInput[T]` (struct-tag binding), embedded `Params` and `Body[T]` lazy body views,
`ParseInput`, `WithBodyDecoder`, `ErrInvalidParamsUsage`,
`ErrMultipleBodyFields`, `ErrBodyFieldMustBeValue` have all been removed
with no shim.

Rationale: The struct-tag binding path was the largest single source of
per-request overhead (struct reflection construction + eager state
building), and it semantically duplicated the explicit-descriptor +
zero-reflection new API. Migration:

```go
// Old: tag binding
type In struct {
    ID int64 `path:"id"`
}
Handle(Get("/users/{id}"), StructInput[In](), JSONOutput[User](), h)

// New: explicit descriptor + MapInputs
Handle(Get("/users/{id}"),
    MapInputs(PathInt64("id"), func(id int64) In { return In{ID: id} }),
    JSONOutput[User](), h)
```

Middleware still uses `Server.MatchedParams(r)` (returns a `Params` request
view) to read path parameters on demand. Arbitrary input uses `InputFunc`;
multi-parameter structs use `MapInputs/MapInputs3..7`.

This is an intentional pre-v1 breaking removal. Callers must migrate the endpoint as a whole and cannot mix the two models.

## License

Same as the [gk](https://github.com/sofiworker/gk) project.

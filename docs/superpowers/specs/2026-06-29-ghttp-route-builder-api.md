# ghttp RouteBuilder API Design

## Goal

`ghttp` should expose one primary server-side route registration style:

```go
ghttp.Route[CreateUserReq, UserResp](app).
	POST("/users/{id}").
	Doc("Create user").
	Tags("Users").
	OperationID("createUser").
	Reads(CreateUserReq{}).
	Responds(http.StatusCreated).With(UserResp{}).Desc("Created").End().
	To(createUser)
```

The public API should prefer this generic `Route[Req, Resp](target).METHOD(path)` builder style for typed handlers and OpenAPI metadata. Gin-style shortcut functions and group-specific shortcut functions should not be the main route registration surface.

## Rationale

Go versions before generic methods do not allow this shape:

```go
app.Route("/users/{id}").
	POST().
	To[CreateUserReq, UserResp](createUser)
```

Because `To[Req, Resp]` would require method-specific type parameters. The current viable shape is to put the type parameters on the package-level `Route` constructor:

```go
ghttp.Route[Req, Resp](target).POST(path).To(handler)
```

This keeps each route strongly typed without making `Server` generic. A generic `Server[Req, Resp]` would be the wrong model because one server naturally owns many routes with different request and response types.

The route path should be supplied to the HTTP method selector, not split between `Route(target, basePath)` and `POST(subpath)`. Prefix reuse belongs to `Group`.

## API Direction

### Keep As Primary API

Typed route registration:

```go
ghttp.Route[Req, Resp](app).GET("/path").To(handler)
ghttp.Route[Req, Resp](group).POST("/path").To(handler)
```

Builder metadata:

```go
.Doc("...")
.Tags("...")
.OperationID("...")
.Reads(Req{})
.Responds(status).With(Resp{}).Desc("...").End()
```

HTTP method selection:

```go
.GET(path)
.POST(path)
.PUT(path)
.DELETE(path)
.PATCH(path)
.HEAD(path)
.OPTIONS(path)
.CONNECT(path)
.TRACE(path)
```

The method selector sets both the HTTP method and the route path.

Raw handler registration:

```go
app.Handle(method, path, httpHandler)
group.Handle(method, path, httpHandler)
```

Low-level router access may remain available for advanced use:

```go
app.Router().Register(method, path, httpHandler)
```

But it is an escape hatch, not the recommended application API.

### Remove Or Deprecate As Primary API

Package-level typed shortcuts:

```go
ghttp.Get[Req, Resp](app, path, handler)
ghttp.Post[Req, Resp](app, path, handler)
ghttp.Put[Req, Resp](app, path, handler)
ghttp.Delete[Req, Resp](app, path, handler)
ghttp.Patch[Req, Resp](app, path, handler)
```

Group-specific typed shortcuts:

```go
ghttp.GroupGet[Req, Resp](group, path, handler)
ghttp.GroupPost[Req, Resp](group, path, handler)
ghttp.GroupPut[Req, Resp](group, path, handler)
ghttp.GroupDelete[Req, Resp](group, path, handler)
```

These APIs create a second registration style and make `Server` and `Group` feel different. The `Group` concept should only add prefix and middleware composition; it should not require a separate function namespace.

## Server, Group, And Router Responsibilities

### Server

`Server` owns runtime dependencies:

- router engine
- codec manager
- validator
- envelope
- OpenAPI collector
- global middleware
- HTTP server lifecycle

`Server` should behave as the root route target.

```go
app := ghttp.New()

ghttp.Route[Req, Resp](app).GET("/users/{id}").To(handler)
```

### Group

`Group` is only a scoped route target:

- path prefix
- group middleware
- reference to the owning server

Groups should use the same builder entry point as the root server:

```go
api := app.Group("/api", authMiddleware)

ghttp.Route[Req, Resp](api).GET("/users/{id}").To(handler)
```

The final registered path is:

```text
/api/users/{id}
```

Group middleware should wrap only routes registered through that group or its children.

### Router

`Router` should remain a small internal routing engine boundary:

```go
type Router interface {
	http.Handler
	Register(method, path string, handler http.Handler) error
}
```

The router should not know about:

- generics
- request or response types
- OpenAPI metadata
- groups
- route builders
- validation
- envelopes

It only stores and dispatches `method + path + http.Handler`.

The default implementation should be `RadixRouter`. `StdRouter` can remain as an adapter if useful, but the user-facing model should not be shaped around router pluggability.

## Route Target Contract

To make `Route[Req, Resp](target)` work with both `*Server` and `*Group`, introduce an internal small interface similar to:

```go
type routeTarget interface {
	handleRoute(method, path string, handler http.Handler, mws ...MiddlewareFunc) error
	addRouteSpec(method, path, doc string, tags []string, operationID string, reqType reflect.Type, responses []responseSpec)
}
```

The exact names can stay unexported. The important part is that `Route` depends on the application-level target, not directly on `*Server`.

`Server` implementation:

```go
func (s *Server) handleRoute(method, path string, handler http.Handler, mws ...MiddlewareFunc) error {
	return s.Handle(method, path, handler, mws...)
}
```

`Group` implementation:

```go
func (g *Group) handleRoute(method, path string, handler http.Handler, mws ...MiddlewareFunc) error {
	all := make([]MiddlewareFunc, 0, len(g.middlewares)+len(mws))
	all = append(all, g.middlewares...)
	all = append(all, mws...)
	return g.server.Handle(method, JoinPaths(g.prefix, path), handler, all...)
}
```

The OpenAPI path must match the final registered path. Therefore `addRouteSpec` should also apply the group prefix when the target is a group.

## Error Handling

`RouteBuilder.To` should return `error`.

Current behavior ignores router registration failures:

```go
_ = b.server.router.Register(...)
```

The builder should surface duplicate route errors, invalid path errors, nil handler errors, and router-specific validation errors:

```go
if err := ghttp.Route[Req, Resp](app).
	POST("/users/{id}").
	To(handler); err != nil {
	return err
}
```

For tests and examples, ignoring the error is still possible:

```go
_ = ghttp.Route[Req, Resp](app).POST("/users/{id}").To(handler)
```

This is a source-level breaking change for existing call sites that currently use `To(handler)` as a statement. The migration is mechanical: either handle the error or assign it to `_`.

## Middleware

Middleware should compose in this order:

1. server middleware
2. group middleware from outer to inner
3. route middleware
4. typed route handler

Route-level middleware can remain available through a builder method:

```go
ghttp.Route[Req, Resp](api).
	GET("/users/{id}").
	Use(auditMiddleware).
	To(handler)
```

If `Use` does not exist yet on `RouteBuilder`, add it rather than preserving shortcut `RouteOption` APIs only for deprecated shortcuts.

## OpenAPI

OpenAPI metadata should be collected from the builder at `To` time after the final route path is known and after router registration succeeds. Failed route registrations must not leave stale OpenAPI entries.

Recommended behavior:

- `Doc` maps to operation description or summary according to current OpenAPI conventions.
- `Tags` maps to operation tags.
- `OperationID` maps to `operationId`.
- `Reads` records the request type.
- `Responds(...).With(...).Desc(...).End()` records response status, schema type, and description.

The low-level `Router.Register` path should not add OpenAPI metadata.

## Migration Plan

### Phase 1: Document And Introduce Unified Target

- Document `Route[Req, Resp](target).METHOD(path)` as the only primary typed route API.
- Make `Route` accept both `*Server` and `*Group`.
- Move route path selection to the HTTP method selector.
- Make `RouteBuilder.To` return `error`.
- Add focused tests for server routes, group routes, middleware order, and OpenAPI path prefixing.

### Phase 2: Deprecate Shortcut APIs

Mark the shortcut typed functions as deprecated:

```go
// Deprecated: use Route[Req, Resp](target).GET(path).To(handler).
func Get[Req, Resp any](...)
```

Do the same for `GroupGet`, `GroupPost`, and related functions.

### Phase 3: Remove Shortcut APIs In A Breaking Release

Remove shortcut functions when the package is ready for a breaking API revision. Keep `Handle`, `Raw`, and `Router` access for raw and advanced use.

### Phase 4: Add Go Generic-Method API Later

When the project moves to a Go version that supports generic methods on concrete types, add the natural method API:

```go
app.Route().
	POST("/users/{id}").
	To[CreateUserReq, UserResp](handler)

api.Route().
	GET("/users/{id}").
	To[GetUserReq, UserResp](handler)
```

At that point `RouteBuilder` can become non-generic, with generics only on `To`.

The existing API can remain as a compatibility wrapper:

```go
ghttp.Route[Req, Resp](target).GET(path).To(handler)
```

## Non-Goals

- Do not make `Server` generic.
- Do not make `Router` generic.
- Do not make generic methods part of exported interfaces.
- Do not design the primary API around `server.GET` or `group.GET` before generic methods are available.
- Do not duplicate every route registration path across package-level shortcuts, group shortcuts, and builder methods.

## Review Checklist

- There is one recommended typed route registration style.
- `Server` and `Group` use the same route builder entry point.
- `Group` only adds prefix and middleware semantics.
- `Router` remains a narrow `net/http` compatible dispatch boundary.
- `RouteBuilder.To` returns registration errors.
- OpenAPI metadata uses the final route path.
- The future generic-method migration does not require changing the router engine.

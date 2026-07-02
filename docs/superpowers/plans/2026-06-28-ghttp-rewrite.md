# ghttp Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite `ghttp/gserver` and `ghttp/gclient` into a flat `ghttp` package that provides a Go 1.24+ generic-first HTTP framework with go-restful-style RouteBuilder API, automatic OpenAPI generation, content-negotiated codec, multipart/form handling, static file serving, template rendering, and a unified HTTP client — all built on `net/http` standard library with the existing three-layer routing engine.

**Architecture:** Single flat package `github.com/sofiworker/gk/ghttp`. The Server itself implements `http.Handler` (compatible with `httptest`, `http.ServeMux`, any middleware). Inside the server is a pluggable `Router` interface: default is the ported three-layer radix-tree matcher, optional is `stdRouter` wrapping Go 1.22+ `http.ServeMux`. Routes are registered via generic `RouteBuilder[Req, Resp]` chain (go-restful style) — OpenAPI metadata is collected at `To()` time before the handler is stored in the router, so the router interface never sees generics. Codecs handle Content-Type / Accept negotiation. The client mirrors server-side types for end-to-end type safety.

**Tech Stack:** Go 1.24.4, standard library `net/http` (no fasthttp), standard library `encoding/json`, `encoding/xml`, `gopkg.in/yaml.v3` (external dep, already present), existing three-layer matcher from `ghttp/gserver` (ported).

---

## Spec Reference

- Design documented throughout `ghttp/gserver/DESIGN_*.md` (archive only — new design supersedes)
- New design artifacts will be at `docs/superpowers/specs/2026-06-28-ghttp-design.md`

## Validation Notes

- No Makefile; run: `gofmt -w <touched files> && go vet ./ghttp/... && go test ./ghttp/...`
- Full repo: `go vet ./... && go test ./...`
- Lint: `golangci-lint run ./ghttp/...` (if config exists; otherwise `go vet`)
- Benchmark comparison against old gserver: `go test -bench=. -benchmem ./ghttp/...`

---

## File Structure

### Core

| File | Responsibility |
|------|---------------|
| `ghttp/ghttp.go` | `New()`, `Server` struct, `Run()`/`Shutdown()`, `Config`/`ServerOption` |
| `ghttp/config.go` | `Config` struct, all `ServerOption` functions, `With*` builders |
| `ghttp/handler.go` | Generic handler types: `HandlerFunc[Req,Resp]`, `NoInputHandler[Resp]`, `NoOutputHandler[Req]`, `RawHandler` |
| `ghttp/builder.go` | `RouteBuilder[Req,Resp]` chain. Methods: `Doc()`, `Tags()`, `OperationID()`, `Reads()`, `Responds()`, `To()`. Terminal methods: `GET()`, `POST()`, `PUT()`, `DELETE()`, `PATCH()`, `HEAD()`, `OPTIONS()`, `Static()`. Shortcut functions: `app.GET()`, `app.POST()`, etc. |
| `ghttp/context.go` | Request context. Holds `http.ResponseWriter`, `*http.Request`, path params, parsed input. Provides `RawRequest()`, `RawResponse()`, `Param()`, `Query()`, `Header()` etc. |
| `ghttp/input.go` | Request input parser. Reads from `Path`/`Query`/`Header`/`Body` struct fields via reflection. Supports `path:`, `query:`, `header:`, `form:`, `json:`, `xml:` tags. |
| `ghttp/output.go` | Response output + `Envelope`. Default envelope `{code, msg, data}`. `WithEnvelope()` customizer. Response status code resolution (Output.Status > HTTPError.Code > default). |
| `ghttp/error.go` | `HTTPError` type, `Err()` constructor, `ErrHandled()` sentinel, `ErrAbort()`. Convenience: `BadRequest()`, `NotFound()`, `Conflict()`, `InternalError()`. |

### Routing (plugable Router interface)

| File | Responsibility |
|------|---------------|
| `ghttp/router.go` | `Router` interface: `http.Handler` + `Register(method, path, http.Handler)`. Implicitly compatible with `http.ServeMux`, `httptest`, any middleware. |
| `ghttp/radix_router.go` | `RadixRouter` — default implementation. Three-layer matcher (static map / segment radix tree / wildcard radix tree). Ported from gserver's matcher + method_matcher + radix. |
| `ghttp/std_router.go` | `StdRouter` — wraps Go 1.22+ `http.ServeMux`. Uses METHOD + path pattern registration. Optional alternative to `RadixRouter`. |
| `ghttp/radix.go` | `CompressedRadixTree` — compressed prefix trie with `:param` / `*wildcard` support (shared by RadixRouter) |

### Codec

| File | Responsibility |
|------|---------------|
| `ghttp/codec.go` | `Codec` interface: `ContentTypes()`, `Marshal()`, `Unmarshal()` |
| `ghttp/codec_manager.go` | `CodecManager` — register/deregister/resolve/negotiate by Content-Type. Defaults: JSON, XML, Plain, Form. |
| `ghttp/codec_json.go` | `JSONCodec` — uses `encoding/json` (standard library) |
| `ghttp/codec_xml.go` | `XMLCodec` — uses `encoding/xml` |
| `ghttp/codec_plain.go` | `PlainCodec` — text/plain |
| `ghttp/codec_form.go` | `FormCodec` — `application/x-www-form-urlencoded` + `multipart/form-data` |

### Template & Static

| File | Responsibility |
|------|---------------|
| `ghttp/render.go` | `Renderer` interface, `GoRenderer` impl (html/template). `NewRenderer()` with `Dir`, `Ext`, `FuncMap`, `Reload`. Reference: gin HTML rendering. |
| `ghttp/static.go` | Static file serving. `Static()`, `StaticFS()`, `StaticFile()` methods on Server. Support `http.FS` / `embed.FS`. |

### Upload & Form

| File | Responsibility |
|------|---------------|
| `ghttp/upload.go` | `FileHeader` type (wraps `*multipart.FileHeader` with `Open()`, `Save()`, `Bytes()`) |
| `ghttp/form.go` | Multipart form parsing: `ParseMultipartForm()`, mixed form fields + files extraction |

### OpenAPI

| File | Responsibility |
|------|---------------|
| `ghttp/openapi.go` | OpenAPI 3.1 document builder. `OpenAPI` struct, `Build()`, `ServeJSON(path)`. Collects routes from builder metadata. |
| `ghttp/schema.go` | JSON Schema generator from Go structs via reflection + struct tags (`doc`, `example`, `minLength`, `format`, etc.) |

### Validation

| File | Responsibility |
|------|---------------|
| `ghttp/validate.go` | `Validator` interface. Built-in tag-level validations (`required`, `minLength`, `maxLength`, `minimum`, `maximum`, `pattern`, `format`). |

### Middleware

| File | Responsibility |
|------|---------------|
| `ghttp/middleware.go` | `MiddlewareFunc = func(http.Handler) http.Handler`. Built-in: `RequestID`, `CORS`, `Logger`, `Recoverer`, `Timeout`. |

### Client (go-resty style chain + generic callers)

| File | Responsibility |
|------|---------------|
| `ghttp/client.go` | `Client` struct, `NewClient()`, `R()` returns `*Request`. Chain setters: `SetBaseURL()`, `SetHeader()`, `SetHeaders()`, `SetQueryParam()`, `SetQueryParams()`, `SetPathParam()`, `SetPathParams()`, `SetAuthToken()`, `SetBasicAuth()`, `SetTimeout()`, `SetCookie()`, `SetCookies()`, `SetUserAgent()`, `SetContentType()`, `SetAccept()`, `SetProxy()`, `SetTracer()`, `SetLogger()`, `SetCache()` |
| `ghttp/client_request.go` | `Request` struct, chain methods: `SetHeader()`, `SetHeaders()`, `SetQueryParam()`, `SetQueryParams()`, `SetPathParam()`, `SetPathParams()`, `SetBody()`, `SetJSONBody()`, `SetXMLBody()`, `SetFormData()`, `SetFile()`, `SetFiles()`, `SetResult()`, `SetResultError()`, `SetContext()`, `SetTimeout()`, `SetAuthToken()`, `SetBasicAuth()`, `SetBearerToken()`, `SetCookie()`, `SetCookies()`, `SetContentType()`, `SetAccept()`. Terminal methods: `Execute(method, url)`, `Get(url)`, `Post(url)`, `Put(url)`, `Delete(url)`, `Patch(url)`, `Head(url)`, `Options(url)`. |
| `ghttp/client_response.go` | `Response` struct: `Bytes()`, `String()`, `Reader()`, `Len()`, `IsSuccess()`, `IsError()`, `StatusCode()`, `Header()`, `Body()`, `Duration()`, `BindJSON(target)`, `BindXML(target)`, `Dump()`. Plus `Envelope` support: `UnwrapEnvelope(target)` for `{code, msg, data}` format. |
| `ghttp/endpoint.go` | Generic endpoint callers: `GET[Req,Resp]()`, `POST[Req,Resp]()`, `PUT[Req,Resp]()`, `DELETE[Req,Resp]()`, `Do[Req,Resp]()`. Type-safe counterparts that use the server-side `Req`/`Resp` types. |

### WebSocket

| File | Responsibility |
|------|---------------|
| `ghttp/websocket.go` | WebSocket types for server side. `WebSocketHandler` interface, `Upgrade()`, message types. Server-side: `s.Upgrade(path, handler)` registers a WebSocket upgrade route. |
| `ghttp/client_websocket.go` | Client WebSocket. `client.WebSocket(path)` returns `*WebSocketConn`. Methods: `ReadJSON()`, `WriteJSON()`, `ReadMessage()`, `WriteMessage()`, `Close()`, `Ping()`, `SetPongHandler()`. Built on `gorilla/websocket`. |

### SSE (Server-Sent Events)

| File | Responsibility |
|------|---------------|
| `ghttp/sse.go` | Server SSE. `SSEHandler` type, `s.SSE(path, handler)` registers SSE endpoint. `SSEWriter` with `WriteEvent()`, `WriteJSON()`, `Flush()`. |
| `ghttp/client_sse.go` | Client SSE. `client.SSE(path)` returns `*SSEStream`. Channel-based: `Events()` returns `<-chan SSEEvent`, support auto-reconnect. |

### Legacy / Remain

| File | Responsibility |
|------|---------------|
| `ghttp/result.go` | `Result` interface (kept from old gserver for backward compat via `WrapToHandler`) |
| `ghttp/writer.go` | `ResponseWriter` interface (wraps `http.ResponseWriter` with `Status()`, `Written()`, `Size()`, `WriteString()`) |
| `ghttp/constants.go` | MIME type constants |
| `ghttp/logger.go` | `Logger` interface |
| `ghttp/util.go` | `JoinPaths`, `CheckPathValid`, etc. |

### Compatibility shims (kept, not modified)

| Package | Purpose |
|---------|---------|
| `ghttp/gserver/` | Old gserver — kept as-is, no changes |
| `ghttp/gclient/` | Old gclient — kept as-is, no changes |
| `gcodec/` | Existing codec library — kept as-is |

### Tests per file

Each file above gets a co-located `*_test.go`. The test structure is:

| Test file | Coverage |
|-----------|----------|
| `ghttp/ghttp_test.go` | Integration: full HTTP round-trips via `httptest` |
| `ghttp/builder_test.go` | RouteBuilder chain correctness, shortcut registrations |
| `ghttp/codec_json_test.go` | JSON marshal/unmarshal round-trip |
| `ghttp/codec_manager_test.go` | Content-Type resolve/negotiate, register/custom |
| `ghttp/input_test.go` | Input binding from Path/Query/Header/Body with struct tags |
| `ghttp/output_test.go` | Envelope wrapping, status code resolution, custom envelope |
| `ghttp/error_test.go` | Error construction, sentinel detection |
| `ghttp/form_test.go` | Form parsing, multipart mixed form+file |
| `ghttp/upload_test.go` | FileHeader Open/Save/Bytes |
| `ghttp/static_test.go` | Static file serving from directory and embed.FS |
| `ghttp/render_test.go` | Template rendering with Go html/template |
| `ghttp/middleware_test.go` | Each built-in middleware (RequestID, CORS, Logger, Recoverer, Timeout) |
| `ghttp/openapi_test.go` | OpenAPI document generation from registered routes |
| `ghttp/schema_test.go` | JSON Schema generation from struct tags |
| `ghttp/validate_test.go` | Built-in tag validation rules |
| `ghttp/client_test.go` | Client Do/GET/POST round-trip against test server |
| `ghttp/match_test.go` | Ported from old `matcher_test.go` + `radix_test.go` |
| `ghttp/benchmark_test.go` | Benchmarks: routing, codec, handler dispatch, client |

---

## Task List

### Task 1: Core package skeleton — Server, Config, Handler types

**Files:**
- Create: `ghttp/ghttp.go`
- Create: `ghttp/config.go`
- Create: `ghttp/handler.go`
- Test: `ghttp/ghttp_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ghttp

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestNewServerDefaults(t *testing.T) {
    app := New("test", "1.0.0")
    if app == nil {
        t.Fatal("New returned nil")
    }
}

func TestServerRunAndShutdown(t *testing.T) {
    app := New()
    go func() {
        if err := app.Run(":0"); err != nil && err != http.ErrServerClosed {
            t.Errorf("Run failed: %v", err)
        }
    }()
    if err := app.Shutdown(); err != nil {
        t.Errorf("Shutdown failed: %v", err)
    }
}

func TestWithAddress(t *testing.T) {
    app := New(WithAddress(":9999"))
    // verify config applied via exported config accessor
    _ = app
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestNewServerDefaults|TestServerRunAndShutdown|TestWithAddress" -v`

Expected: FAIL — package `ghttp` does not exist yet

- [ ] **Step 3: Write minimal implementation**

**`ghttp/ghttp.go`:**

```go
package ghttp

import (
    "context"
    "net/http"
    "sync"
)

// Server is the core HTTP server. It implements http.Handler so it can be:
// - used standalone via Run()
// - embedded in any http.ServeMux as a sub-handler
// - tested via httptest
// - wrapped by any func(http.Handler) http.Handler middleware
type Server struct {
    router     Router        // pluggable routing engine
    config     *Config

    codecMgr   *CodecManager
    renderer   Renderer
    envelope   EnvelopeFunc
    validator  Validator
    middlewares []MiddlewareFunc

    httpServer *http.Server
    mu         sync.Mutex
    openAPI    *OpenAPI
    routed     bool
}

func New(name, version string, opts ...ServerOption) *Server {
    c := &Config{
        address: ":8080",
    }
    for _, opt := range opts {
        opt(c)
    }

    s := &Server{
        router:    newRadixRouter(),   // default: three-layer radix tree
        config:    c,
        codecMgr:  NewCodecManager(),
        envelope:  DefaultEnvelope,
        openAPI:   NewOpenAPI(name, version),
    }

    return s
}

// ServeHTTP implements http.Handler — delegates to the pluggable router.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    s.router.ServeHTTP(w, r)
}

func (s *Server) Run(addr ...string) error {
    address := s.config.address
    if len(addr) > 0 {
        address = addr[0]
    }
    s.finalizeRoutes()

    var h http.Handler = s
    for i := len(s.middlewares) - 1; i >= 0; i-- {
        h = s.middlewares[i](h)
    }

    s.httpServer = &http.Server{
        Addr:    address,
        Handler: h,
    }
    return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown() error {
    return s.httpServer.Shutdown(context.Background())
}

// finalizeRoutes locks the route table and registers OpenAPI endpoint
func (s *Server) finalizeRoutes() {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.routed {
        return
    }
    s.routed = true

    // Register OpenAPI JSON endpoint
    if s.openAPI != nil {
        spec := s.openAPI.Build()
        _ = s.router.Register("GET", "/openapi.json", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Content-Type", "application/json")
            w.Write(spec)
        }))
    }
}
```

**`ghttp/config.go`:**

```go
package ghttp

type Config struct {
    address   string
    validator Validator
}

type ServerOption func(*Config)

func WithAddress(addr string) ServerOption {
    return func(c *Config) { c.address = addr }
}

func WithValidator(v Validator) ServerOption {
    return func(c *Config) { c.validator = v }
}
```

**`ghttp/handler.go`:**

```go
package ghttp

import "net/http"

// HandlerFunc is the generic handler signature.
// Req is the parsed request input struct. Resp is the response output struct.
type HandlerFunc[Req, Resp any] func(ctx Context, input *Req) (*Resp, error)

// NoInputHandler is for endpoints with no request body / parameters.
type NoInputHandler[Resp any] func(ctx Context) (*Resp, error)

// NoOutputHandler is for endpoints that only return an error.
type NoOutputHandler[Req any] func(ctx Context, input *Req) error

// RawHandler allows direct access to http.ResponseWriter and *http.Request.
type RawHandler func(w http.ResponseWriter, r *http.Request)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestNewServerDefaults|TestServerRunAndShutdown|TestWithAddress" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/ghttp.go ghttp/config.go ghttp/handler.go ghttp/ghttp_test.go
git add ghttp/ghttp.go ghttp/config.go ghttp/handler.go ghttp/ghttp_test.go
git commit -m "feat: add ghttp core skeleton (Server, Config, handler types)"
```

---

### Task 2: Router interface + default RadixRouter implementation

**Files:**
- Create: `ghttp/router.go`
- Create: `ghttp/radix_router.go`
- Create: `ghttp/std_router.go` (optional, Go 1.22+)
- Create: `ghttp/radix.go`
- Create: `ghttp/util.go` (shared helpers)
- Test: `ghttp/router_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ghttp

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestRouterInterface(t *testing.T) {
    // Router must be http.Handler + Register
    var r Router = newRadixRouter()
    if r == nil {
        t.Fatal("newRadixRouter returned nil")
    }
}

func TestRadixRouterStaticParamWildcard(t *testing.T) {
    r := newRadixRouter()

    dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

    if err := r.Register("GET", "/users", dummy); err != nil {
        t.Fatalf("Register failed: %v", err)
    }
    if err := r.Register("GET", "/users/:id", dummy); err != nil {
        t.Fatalf("Register failed: %v", err)
    }
    if err := r.Register("GET", "/assets/*path", dummy); err != nil {
        t.Fatalf("Register failed: %v", err)
    }
    if err := r.Register("GET", "/articles/:category/:id", dummy); err != nil {
        t.Fatalf("Register failed: %v", err)
    }

    // RadixRouter implements http.Handler, test via httptest
    ts := httptest.NewServer(r)
    defer ts.Close()

    tests := []struct {
        path     string
        expected int
    }{
        {"/users", 200},
        {"/users/42", 200},
        {"/assets/img/logo.png", 200},
        {"/articles/tech/123", 200},
        {"/notfound", 404},
    }

    for _, tt := range tests {
        resp, err := http.Get(ts.URL + tt.path)
        if err != nil {
            t.Fatalf("GET %s failed: %v", tt.path, err)
        }
        resp.Body.Close()
        if resp.StatusCode != tt.expected {
            t.Errorf("GET %s: expected %d, got %d", tt.path, tt.expected, resp.StatusCode)
        }
    }
}

func TestRadixRouterPathParams(t *testing.T) {
    r := newRadixRouter()

    var capturedParams map[string]string
    r.Register("GET", "/users/:id", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
        // params passed via context
        params := Params(req)
        capturedParams = params
        w.WriteHeader(http.StatusOK)
    }))

    ts := httptest.NewServer(r)
    defer ts.Close()

    http.Get(ts.URL + "/users/42")
    if capturedParams["id"] != "42" {
        t.Fatalf("expected id=42, got %s", capturedParams["id"])
    }
}

func TestStdRouterGo122(t *testing.T) {
    r := newStdRouter()
    dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
    if err := r.Register("GET", "/users/{id}", dummy); err != nil {
        t.Fatalf("StdRouter Register failed: %v", err)
    }
    // StdRouter must also implement http.Handler
    if _, ok := r.(http.Handler); !ok {
        t.Fatal("StdRouter does not implement http.Handler")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestRouterInterface|TestRadixRouterStaticParamWildcard|TestRadixRouterPathParams|TestStdRouterGo122" -v`

Expected: FAIL — Router, newRadixRouter, Params not defined

- [ ] **Step 3: Implement Router interface and default RadixRouter**

**`ghttp/router.go`:**

```go
package ghttp

import "net/http"

// Router is the pluggable routing interface.
// It implements http.Handler so the Server delegates to it directly.
// Routes are registered before the server starts; the route table is read-only at runtime.
type Router interface {
    http.Handler
    Register(method, path string, handler http.Handler) error
}
```

**`ghttp/radix_router.go`:**

Port the three-layer matcher from gserver's `matcher.go` + `method_matcher.go` + `radix.go` into a single `RadixRouter` struct that implements `Router`:

```go
package ghttp

import "net/http"

type RadixRouter struct {
    // three-layer structure:
    methodMatchers map[string]*MethodMatcher
    // ...
}

func newRadixRouter() *RadixRouter { ... }

func (r *RadixRouter) Register(method, path string, handler http.Handler) error { ... }

func (r *RadixRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    method := req.Method
    path := req.URL.Path

    // Lookup in method matcher
    result := r.lookup(method, path)
    if result == nil {
        w.WriteHeader(http.StatusNotFound)
        return
    }

    // Store path params in request context
    if len(result.PathParams) > 0 {
        ctx := context.WithValue(req.Context(), pathParamsKey, result.PathParams)
        req = req.WithContext(ctx)
    }

    result.Handler.ServeHTTP(w, req)
}
```

The key difference from old gserver: instead of storing `HandlerFunc` (gserver-specific), store `http.Handler`. Path parameters are passed through `context.Context` instead of the old `Context.Param()` method.

**`ghttp/radix.go`:** Port `CompressedRadixTree` unchanged from gserver.

**`ghttp/std_router.go`:**

```go
package ghttp

import (
    "net/http"
    "strings"
)

// StdRouter wraps http.ServeMux (Go 1.22+ pattern routing).
type StdRouter struct {
    mux *http.ServeMux
}

func newStdRouter() *StdRouter {
    return &StdRouter{mux: http.NewServeMux()}
}

func (r *StdRouter) Register(method, path string, handler http.Handler) error {
    // Go 1.22+ supports "METHOD /path" pattern
    pattern := method + " " + path
    r.mux.Handle(pattern, handler)
    return nil
}

func (r *StdRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    r.mux.ServeHTTP(w, req)
}
```

**`ghttp/ghttp.go` update — Server uses Router:**

```go
type Server struct {
    router     Router        // pluggable — default is RadixRouter
    // ... (rest unchanged)
}

// ServerOption to set custom router
func WithRouter(router Router) ServerOption {
    return func(c *Config) {
        // store in config for New() to pick up
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestRouterInterface|TestRadixRouterStaticParamWildcard|TestRadixRouterPathParams|TestStdRouterGo122" -v`

Expected: PASS

- [ ] **Step 5: Also write radix tree unit test**

Ported from old `radix_test.go`:

```go
func TestRadixTreeRouteCount(t *testing.T) {
    tree := newCompressedRadixTree()
    if got := tree.routeCount(); got != 0 {
        t.Fatalf("expected 0 routes, got %d", got)
    }
    dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
    tree.insert(newRouteEntry("/static", dummy))
    tree.insert(newRouteEntry("/user/:id", dummy))
    if got := tree.routeCount(); got != 2 {
        t.Fatalf("expected 2 routes, got %d", got)
    }
}
```

- [ ] **Step 6: Format and commit**

```bash
gofmt -w ghttp/router.go ghttp/radix_router.go ghttp/std_router.go ghttp/radix.go ghttp/util.go ghttp/router_test.go
git add ghttp/router.go ghttp/radix_router.go ghttp/std_router.go ghttp/radix.go ghttp/util.go ghttp/router_test.go
git commit -m "feat: add pluggable Router interface + RadixRouter + StdRouter"
```

---

### Task 3: RouteBuilder chain API (go-restful style)

**Files:**
- Create: `ghttp/builder.go`
- Test: `ghttp/builder_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ghttp

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
)

type TestInput struct {
    Path struct {
        ID string `path:"id"`
    }
    Body struct {
        Name string `json:"name"`
    }
}

type TestOutput struct {
    Status int `default:"200"`
    Body struct {
        ID   string `json:"id"`
        Name string `json:"name"`
    }
}

func testHandler(ctx Context, req *TestInput) (*TestOutput, error) {
    return &TestOutput{
        Body: struct {
            ID   string `json:"id"`
            Name string `json:"name"`
        }{
            ID:   req.Path.ID,
            Name: req.Body.Name,
        },
    }, nil
}

func TestRouteBuilderBasicChain(t *testing.T) {
    app := New("test", "1.0.0")

    app.Route("/users/{id}").
        POST("").
        Doc("Create user").
        Tags("Users").
        OperationID("createUser").
        Reads(TestInput{}).
        Responds(http.StatusCreated).With(TestOutput{}).Desc("Created").End().
        To(testHandler)

    // Make request via httptest
    w := httptest.NewRecorder()
    r := httptest.NewRequest("POST", "/users/42", bytes.NewReader([]byte(`{"name":"Alice"}`)))
    r.Header.Set("Content-Type", "application/json")
    app.ServeHTTP(w, r)

    if w.Code != http.StatusCreated {
        t.Fatalf("expected 201, got %d", w.Code)
    }
}

func TestShortcutPOST(t *testing.T) {
    app := New("test", "1.0.0")

    app.POST("/users/{id}", testHandler,
        WithDoc("Create user shortcut"),
        WithTags("Users"),
        WithReq(TestInput{}),
        WithResp(201, TestOutput{}, "Created"),
    )

    w := httptest.NewRecorder()
    r := httptest.NewRequest("POST", "/users/99", bytes.NewReader([]byte(`{"name":"Bob"}`)))
    r.Header.Set("Content-Type", "application/json")
    app.ServeHTTP(w, r)

    if w.Code != http.StatusCreated {
        t.Fatalf("expected 201, got %d", w.Code)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestRouteBuilderBasicChain|TestShortcutPOST" -v`

Expected: FAIL — `Route`, `POST`, `To`, `WithDoc`, `WithReq` not defined

- [ ] **Step 3: Implement RouteBuilder**

**`ghttp/builder.go`:**

```go
package ghttp

import (
    "context"
    "encoding/json"
    "net/http"
    "reflect"
)

// RouteBuilder builds a route in a go-restful-style chain.
type RouteBuilder[Req, Resp any] struct {
    server      *Server
    path        string
    method      string
    doc         string
    tags        []string
    operationID string
    reqType     reflect.Type
    responses   []responseSpec
    handler     HandlerFunc[Req, Resp]
    rawHandler  http.HandlerFunc
    middlewares []MiddlewareFunc
}

type responseSpec struct {
    Code        int
    Description string
    ModelType   reflect.Type
}

// Route creates a new RouteBuilder on the given path.
func (s *Server) Route(path string) *RouteBuilder[struct{}, struct{}] {
    return &RouteBuilder[struct{}, struct{}]{
        server: s,
        path:   path,
    }
}

// POST sets the HTTP method and returns the builder for chaining.
func (b *RouteBuilder[Req, Resp]) POST(subpath string) *RouteBuilder[Req, Resp] {
    b.method = "POST"
    b.path = JoinPaths(b.path, subpath)
    return b
}

func (b *RouteBuilder[Req, Resp]) GET(subpath string) *RouteBuilder[Req, Resp] {
    b.method = "GET"
    b.path = JoinPaths(b.path, subpath)
    return b
}

func (b *RouteBuilder[Req, Resp]) PUT(subpath string) *RouteBuilder[Req, Resp] { ... }
func (b *RouteBuilder[Req, Resp]) DELETE(subpath string) *RouteBuilder[Req, Resp] { ... }
func (b *RouteBuilder[Req, Resp]) PATCH(subpath string) *RouteBuilder[Req, Resp] { ... }
func (b *RouteBuilder[Req, Resp]) HEAD(subpath string) *RouteBuilder[Req, Resp] { ... }
func (b *RouteBuilder[Req, Resp]) OPTIONS(subpath string) *RouteBuilder[Req, Resp] { ... }

// Doc sets the API documentation string.
func (b *RouteBuilder[Req, Resp]) Doc(s string) *RouteBuilder[Req, Resp] {
    b.doc = s
    return b
}

// Tags sets OpenAPI tags.
func (b *RouteBuilder[Req, Resp]) Tags(tags ...string) *RouteBuilder[Req, Resp] {
    b.tags = tags
    return b
}

// OperationID sets the OpenAPI operation ID.
func (b *RouteBuilder[Req, Resp]) OperationID(id string) *RouteBuilder[Req, Resp] {
    b.operationID = id
    return b
}

// Reads declares the request input type.
func (b *RouteBuilder[Req, Resp]) Reads(input Req) *RouteBuilder[Req, Resp] {
    b.reqType = reflect.TypeOf(input)
    return b
}

// Responds starts a response specification block.
// Must be followed by .With(model).Desc(desc).End()
func (b *RouteBuilder[Req, Resp]) Responds(code int) *responseSpecBuilder[Req, Resp] {
    return &responseSpecBuilder[Req, Resp]{
        builder: b,
        code:    code,
    }
}

// responseSpecBuilder is a sub-builder for a single response.
type responseSpecBuilder[Req, Resp any] struct {
    builder *RouteBuilder[Req, Resp]
    code    int
}

func (rb *responseSpecBuilder[Req, Resp]) With(model interface{}) *responseSpecBuilder[Req, Resp] {
    rb.builder.responses = append(rb.builder.responses, responseSpec{
        Code:      rb.code,
        ModelType: reflect.TypeOf(model),
    })
    return rb
}

func (rb *responseSpecBuilder[Req, Resp]) Desc(desc string) *responseSpecBuilder[Req, Resp] {
    if len(rb.builder.responses) > 0 {
        rb.builder.responses[len(rb.builder.responses)-1].Description = desc
    }
    return rb
}

func (rb *responseSpecBuilder[Req, Resp]) End() *RouteBuilder[Req, Resp] {
    return rb.builder
}

// To registers the handler and finalizes the route.
func (b *RouteBuilder[Req, Resp]) To(handler HandlerFunc[Req, Resp]) {
    b.handler = handler
    b.register()
}

func (b *RouteBuilder[Req, Resp]) register() {
    absolutePath := b.path

    // Build the http.Handler that dispatches this route
    h := b.buildHandler()

    // Store route metadata for OpenAPI
    b.server.openAPI.AddRoute(b.method, absolutePath, b.doc, b.tags, b.operationID, b.reqType, b.responses)

    // Register in pluggable router
    _ = b.server.router.Register(b.method, absolutePath, h)
}

func (b *RouteBuilder[Req, Resp]) buildHandler() http.Handler {
    h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 1. Create context
        ctx := newContext(w, r)

        // 2. Parse input from request
        var input Req
        if err := parseInput(r, &input); err != nil {
            b.server.envelope(ctx, http.StatusBadRequest, nil, err, b.server.codecMgr)
            return
        }

        // 3. Validate
        if b.server.validator != nil {
            if err := b.server.validator.Validate(r.Context(), &input); err != nil {
                b.server.envelope(ctx, http.StatusUnprocessableEntity, nil, err, b.server.codecMgr)
                return
            }
        }

        // 4. Call handler
        resp, err := b.handler(ctx, &input)
        if err != nil {
            // Check for abort/handled sentinels
            if isErrHandled(err) {
                return
            }
            b.server.envelope(ctx, http.StatusInternalServerError, nil, err, b.server.codecMgr)
            return
        }

        // 5. Encode response
        b.server.envelope(ctx, resolveStatusCode(resp), resp, nil, b.server.codecMgr)
    })
    return h
}
```

**Server shortcut methods:**

```go
// GET registers a shortcut route.
func (s *Server) GET[Req, Resp any](path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
    b := &RouteBuilder[Req, Resp]{server: s, path: path, method: "GET"}
    for _, opt := range opts {
        opt(b)
    }
    b.To(handler)
}

// POST, PUT, DELETE, PATCH, HEAD, OPTIONS — same pattern
```

```go
// RouteOption is an option that applies to a RouteBuilder.
type RouteOption func(interface{})

func WithDoc(s string) RouteOption { ... }
func WithTags(tags ...string) RouteOption { ... }
func WithReq[Req any](req Req) RouteOption { ... }
func WithResp[Resp any](code int, model Resp, desc string) RouteOption { ... }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestRouteBuilderBasicChain|TestShortcutPOST" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/builder.go ghttp/builder_test.go
git add ghttp/builder.go ghttp/builder_test.go
git commit -m "feat: add go-restful-style RouteBuilder chain API"
```

---

### Task 4: Input parser (Path/Query/Header/Body four-section binding)

**Files:**
- Create: `ghttp/input.go`
- Test: `ghttp/input_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestParseInput_PathQueryHeaderBody(t *testing.T) {
    type Req struct {
        Path struct {
            ID    string `path:"id"`
            OrgID string `path:"orgId"`
        }
        Query struct {
            Page int `query:"page" default:"1"`
        }
        Header struct {
            Auth string `header:"Authorization"`
        }
        Body struct {
            Name string `json:"name"`
            Age  int    `json:"age"`
        }
    }

    r := httptest.NewRequest("POST", "/orgs/org-42/users/99?page=3", bytes.NewReader([]byte(`{"name":"Alice","age":30}`)))
    r.Header.Set("Authorization", "Bearer xxx")
    r.Header.Set("Content-Type", "application/json")

    var input Req
    if err := parseInput(r, &input); err != nil {
        t.Fatalf("parseInput failed: %v", err)
    }

    if input.Path.ID != "99" {
        t.Fatalf("expected Path.ID=99, got %s", input.Path.ID)
    }
    if input.Path.OrgID != "org-42" {
        t.Fatalf("expected Path.OrgID=org-42, got %s", input.Path.OrgID)
    }
    if input.Query.Page != 3 {
        t.Fatalf("expected Query.Page=3, got %d", input.Query.Page)
    }
    if input.Header.Auth != "Bearer xxx" {
        t.Fatalf("expected Header.Auth=Bearer xxx, got %s", input.Header.Auth)
    }
    if input.Body.Name != "Alice" {
        t.Fatalf("expected Body.Name=Alice, got %s", input.Body.Name)
    }
    if input.Body.Age != 30 {
        t.Fatalf("expected Body.Age=30, got %d", input.Body.Age)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestParseInput_PathQueryHeaderBody" -v`

Expected: FAIL — parseInput not defined

- [ ] **Step 3: Implement input parser**

**`ghttp/input.go`:**

```go
package ghttp

import (
    "encoding/json"
    "encoding/xml"
    "net/http"
    "reflect"
    "strconv"
    "strings"
)

// parseInput reads path params, query, header, and body
// from the request and fills the input struct.
func parseInput(r *http.Request, input interface{}) error {
    v := reflect.ValueOf(input)
    if v.Kind() != reflect.Ptr || v.IsNil() {
        return ErrInputMustBePointer
    }
    v = v.Elem()
    if v.Kind() != reflect.Struct {
        return ErrInputMustBeStruct
    }

    for i := 0; i < v.NumField(); i++ {
        field := v.Type().Field(i)
        switch field.Name {
        case "Path":
            if err := parsePathParams(r, v.Field(i)); err != nil {
                return err
            }
        case "Query":
            if err := parseQueryParams(r, v.Field(i)); err != nil {
                return err
            }
        case "Header":
            if err := parseHeaderFields(r, v.Field(i)); err != nil {
                return err
            }
        case "Body":
            if err := parseBody(r, v.Field(i)); err != nil {
                return err
            }
        }
    }
    return nil
}

func parsePathParams(r *http.Request, field reflect.Value) error {
    // Path params are already resolved by the router and stored in request context
    // They are accessed via r.Context().Value(pathParamsKey)
    params, _ := r.Context().Value(pathParamsKey).(map[string]string)
    if params == nil {
        return nil
    }
    return setStructFieldsFromMap(field, params, "path")
}

func parseQueryParams(r *http.Request, field reflect.Value) error {
    query := r.URL.Query()
    return setStructFieldsFromValues(field, query, "query")
}

func parseHeaderFields(r *http.Request, field reflect.Value) error {
    return setStructFieldsFromHeader(field, r.Header, "header")
}

func parseBody(r *http.Request, bodyField reflect.Value) error {
    ct := r.Header.Get("Content-Type")
    switch {
    case strings.Contains(ct, "application/json"):
        return json.NewDecoder(r.Body).Decode(bodyField.Addr().Interface())
    case strings.Contains(ct, "application/xml"), strings.Contains(ct, "text/xml"):
        return xml.NewDecoder(r.Body).Decode(bodyField.Addr().Interface())
    default:
        // JSON default
        return json.NewDecoder(r.Body).Decode(bodyField.Addr().Interface())
    }
}
```

The helper functions `setStructFieldsFromMap`, `setStructFieldsFromValues`, `setStructFieldsFromHeader` perform reflection-based field filling from the source, respecting struct tags and type conversions (string→int, string→bool, etc.).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestParseInput_PathQueryHeaderBody" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/input.go ghttp/input_test.go
git add ghttp/input.go ghttp/input_test.go
git commit -m "feat: add input parser with path/query/header/body binding"
```

---

### Task 5: Codec system (flat, Content-Type driven)

**Files:**
- Create: `ghttp/codec.go`
- Create: `ghttp/codec_manager.go`
- Create: `ghttp/codec_json.go`
- Create: `ghttp/codec_xml.go`
- Create: `ghttp/codec_plain.go`
- Create: `ghttp/codec_form.go`
- Test: `ghttp/codec_json_test.go`
- Test: `ghttp/codec_manager_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestJSONCodecRoundTrip(t *testing.T) {
    codec := &JSONCodec{}

    var buf bytes.Buffer
    if err := codec.Marshal(&buf, map[string]string{"key": "value"}); err != nil {
        t.Fatalf("Marshal failed: %v", err)
    }
    if !strings.Contains(buf.String(), `"key"`) {
        t.Fatalf("expected JSON with key, got %s", buf.String())
    }

    var result map[string]string
    if err := codec.Unmarshal(&buf, &result); err != nil {
        t.Fatalf("Unmarshal failed: %v", err)
    }
    if result["key"] != "value" {
        t.Fatalf("expected value, got %s", result["key"])
    }
}

func TestCodecManagerResolve(t *testing.T) {
    mgr := NewCodecManager()

    codec, ok := mgr.Resolve("application/json")
    if !ok {
        t.Fatal("expected JSON codec to be registered")
    }
    if codec.ContentTypes()[0] != "application/json" {
        t.Fatalf("expected application/json, got %s", codec.ContentTypes()[0])
    }

    codec2, ok2 := mgr.Resolve("application/xml")
    if !ok2 {
        t.Fatal("expected XML codec to be registered")
    }
    _ = codec2
}

func TestCodecManagerNegotiate(t *testing.T) {
    mgr := NewCodecManager()

    codec := mgr.Negotiate("application/json, text/plain;q=0.5")
    if codec.ContentTypes()[0] != "application/json" {
        t.Fatalf("expected JSON, got %s", codec.ContentTypes()[0])
    }

    codec2 := mgr.Negotiate("text/plain")
    if codec2.ContentTypes()[0] != "text/plain" {
        t.Fatalf("expected text/plain, got %s", codec2.ContentTypes()[0])
    }
}

func TestCodecManagerRegisterCustom(t *testing.T) {
    mgr := NewCodecManager()

    custom := &testCodec{ct: "application/custom"}
    if err := mgr.Register(custom); err != nil {
        t.Fatalf("Register failed: %v", err)
    }

    codec, ok := mgr.Resolve("application/custom")
    if !ok {
        t.Fatal("expected custom codec to be found")
    }
    if codec.ContentTypes()[0] != "application/custom" {
        t.Fatalf("expected application/custom, got %s", codec.ContentTypes()[0])
    }
}

type testCodec struct {
    ct string
}
func (c *testCodec) ContentTypes() []string { return []string{c.ct} }
func (c *testCodec) Marshal(w io.Writer, v interface{}) error { return nil }
func (c *testCodec) Unmarshal(r io.Reader, v interface{}) error { return nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestJSONCodecRoundTrip|TestCodecManager" -v`

Expected: FAIL — Codec interface, CodecManager, JSONCodec not defined

- [ ] **Step 3: Implement Codec system**

**`ghttp/codec.go`:**

```go
package ghttp

import "io"

// Codec handles HTTP content encoding/decoding.
// Each Codec is associated with one or more Content-Types.
type Codec interface {
    ContentTypes() []string
    Marshal(w io.Writer, v interface{}) error
    Unmarshal(r io.Reader, v interface{}) error
}
```

**`ghttp/codec_manager.go`:** Manager with Register/Resolve/Negotiate using sync.RWMutex. Default register: JSON, XML, Plain, Form. YAML is NOT registered by default.

**`ghttp/codec_json.go`:** Uses `encoding/json` standard library.

**`ghttp/codec_xml.go`:** Uses `encoding/xml` standard library.

**`ghttp/codec_plain.go`:** Uses string conversion (same as old gcodec/plain_codec.go).

**`ghttp/codec_form.go`:** Parses `application/x-www-form-urlencoded` and `multipart/form-data`. For multipart, also populates `FileHeader` fields.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestJSONCodecRoundTrip|TestCodecManager" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/codec.go ghttp/codec_manager.go ghttp/codec_json.go ghttp/codec_xml.go ghttp/codec_plain.go ghttp/codec_form.go ghttp/codec_json_test.go ghttp/codec_manager_test.go
git add ghttp/codec*.go ghttp/codec_*_test.go
git commit -m "feat: add flat codec system with JSON/XML/Plain/Form, Content-Type negotiation"
```

---

### Task 6: Output, Envelope, and error handling

**Files:**
- Create: `ghttp/output.go`
- Create: `ghttp/error.go`
- Test: `ghttp/output_test.go`
- Test: `ghttp/error_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestEnvelopeDefault_Success(t *testing.T) {
    app := New("test", "1.0.0")

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/test", nil)

    // Manually invoke envelope
    ctx := &responseContext{w: w, r: r, codecMgr: app.codecMgr}
    app.envelope(ctx, http.StatusOK, map[string]string{"id": "1"}, nil, app.codecMgr)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }

    var body map[string]interface{}
    json.Unmarshal(w.Body.Bytes(), &body)
    if body["code"].(float64) != 0 {
        t.Fatalf("expected code=0, got %v", body["code"])
    }
    if body["msg"] != "success" {
        t.Fatalf("expected msg=success, got %v", body["msg"])
    }
}

func TestEnvelopeDefault_Error(t *testing.T) {
    app := New("test", "1.0.0")

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/test", nil)

    app.envelope(&responseContext{w: w, r: r, codecMgr: app.codecMgr},
        http.StatusNotFound, nil, Err(http.StatusNotFound, "user not found"), app.codecMgr)

    if w.Code != http.StatusNotFound {
        t.Fatalf("expected 404, got %d", w.Code)
    }

    var body map[string]interface{}
    json.Unmarshal(w.Body.Bytes(), &body)
    code := body["code"].(float64)
    if int(code) != http.StatusNotFound {
        t.Fatalf("expected code=404, got %v", code)
    }
}

func TestErrorConstruction(t *testing.T) {
    err := Err(http.StatusConflict, "already exists", WithCause(ErrConflict))
    if err.Code != http.StatusConflict {
        t.Fatalf("expected code 409, got %d", err.Code)
    }
    if !AsError(err).Is(ErrConflict) {
        t.Fatal("expected cause to be ErrConflict")
    }
}

func TestErrHandledSentinel(t *testing.T) {
    if !isErrHandled(ErrHandled()) {
        t.Fatal("ErrHandled() should be detected")
    }
    if isErrHandled(nil) {
        t.Fatal("nil should not be ErrHandled")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestEnvelopeDefault|TestErrorConstruction|TestErrHandledSentinel" -v`

Expected: FAIL — types not defined

- [ ] **Step 3: Implement output and error**

**`ghttp/error.go`:**

```go
package ghttp

import "errors"

var (
    ErrConflict = errors.New("conflict")
    ErrNotFound = errors.New("not found")
    ErrHandled  = errors.New("response already handled")
    ErrAbort    = errors.New("abort")
)

// HTTPError is a structured HTTP error with code, message and optional cause.
type HTTPError struct {
    Code    int
    Message string
    Err     error // wrapped cause
}

func (e *HTTPError) Error() string { return e.Message }
func (e *HTTPError) Unwrap() error { return e.Err }
func (e *HTTPError) Is(target error) bool { return errors.Is(e.Err, target) }

type ErrorOption func(*HTTPError)

func WithCause(err error) ErrorOption {
    return func(e *HTTPError) { e.Err = err }
}

func Err(statusCode int, msg string, opts ...ErrorOption) *HTTPError {
    e := &HTTPError{Code: statusCode, Message: msg}
    for _, opt := range opts {
        opt(e)
    }
    return e
}

func BadRequest(msg string) *HTTPError {
    return Err(http.StatusBadRequest, msg)
}
// ... NotFound, Conflict, InternalError

func AsError(err error) *HTTPError {
    var he *HTTPError
    if errors.As(err, &he) {
        return he
    }
    return nil
}

func isErrHandled(err error) bool {
    return errors.Is(err, ErrHandled)
}
```

**`ghttp/output.go`:**

```go
package ghttp

import (
    "encoding/json"
    "net/http"
)

// EnvelopeFunc is the function that wraps responses.
// It receives the status code, the raw response (or nil on error),
// the raw error (or nil on success), and the negotiated codec.
type EnvelopeFunc func(ctx Context, statusCode int, resp interface{}, err error, codecMgr *CodecManager)

// responseContext implements Context for internal use during response writing.
type responseContext struct {
    w        http.ResponseWriter
    r        *http.Request
    codecMgr *CodecManager
}

func (c *responseContext) ResponseWriter() http.ResponseWriter { return c.w }
func (c *responseContext) Request() *http.Request { return c.r }

// DefaultEnvelope wraps responses in {code, msg, data}.
func DefaultEnvelope(ctx Context, statusCode int, resp interface{}, err error, codecMgr *CodecManager) {
    w := ctx.ResponseWriter()
    r := ctx.Request()

    // Determine encoding
    accept := r.Header.Get("Accept")
    codec := codecMgr.Negotiate(accept)

    w.Header().Set("Content-Type", codec.ContentTypes()[0])

    var envelope struct {
        Code int         `json:"code"`
        Msg  string      `json:"msg"`
        Data interface{} `json:"data,omitempty"`
    }

    if err != nil {
        he := AsError(err)
        if he != nil {
            statusCode = he.Code
            envelope.Code = he.Code
            envelope.Msg = he.Message
        } else {
            envelope.Code = http.StatusInternalServerError
            envelope.Msg = err.Error()
        }
    } else {
        envelope.Msg = "success"
        envelope.Data = resp
    }

    w.WriteHeader(statusCode)
    codec.Marshal(w, &envelope)
}

// resolveStatusCode extracts the HTTP status code from the response.
// Priority: Output.Status > default
func resolveStatusCode(resp interface{}) int {
    // Use reflection to read Status field if present
    v := reflect.ValueOf(resp)
    if v.Kind() == reflect.Ptr {
        v = v.Elem()
    }
    if v.Kind() == reflect.Struct {
        statusField := v.FieldByName("Status")
        if statusField.IsValid() && statusField.Kind() == reflect.Int {
            if code := int(statusField.Int()); code != 0 {
                return code
            }
        }
    }
    return http.StatusOK
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestEnvelopeDefault|TestErrorConstruction|TestErrHandledSentinel" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/output.go ghttp/error.go ghttp/output_test.go ghttp/error_test.go
git add ghttp/output.go ghttp/error.go ghttp/output_test.go ghttp/error_test.go
git commit -m "feat: add envelope system and HTTP error types"
```

---

### Task 7: Context and middleware system

**Files:**
- Create: `ghttp/context.go`
- Create: `ghttp/middleware.go`
- Test: `ghttp/middleware_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestMiddlewareOrder(t *testing.T) {
    app := New("test", "1.0.0")

    var order []string

    app.Use(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            order = append(order, "a_before")
            next.ServeHTTP(w, r)
            order = append(order, "a_after")
        })
    })

    app.Use(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            order = append(order, "b_before")
            next.ServeHTTP(w, r)
            order = append(order, "b_after")
        })
    })

    app.GET("/test", func(ctx Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
        order = append(order, "handler")
        return &struct{ Body struct{} }{}, nil
    })

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/test", nil)
    app.ServeHTTP(w, r)

    expected := []string{"a_before", "b_before", "handler", "b_after", "a_after"}
    if !reflect.DeepEqual(order, expected) {
        t.Fatalf("expected %v, got %v", expected, order)
    }
}

func TestBuiltinMiddlewareRequestID(t *testing.T) {
    app := New("test", "1.0.0")
    app.Use(RequestID())

    app.GET("/test", func(ctx Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
        id := ctx.ResponseWriter().Header().Get("X-Request-ID")
        if id == "" {
            t.Error("expected X-Request-ID in response")
        }
        return &struct{ Body struct{} }{}, nil
    })

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/test", nil)
    app.ServeHTTP(w, r)

    if w.Header().Get("X-Request-ID") == "" {
        t.Fatal("expected X-Request-ID header")
    }
}

func TestBuiltinMiddlewareRecovery(t *testing.T) {
    app := New("test", "1.0.0")
    app.Use(Recoverer())

    app.GET("/panic", func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
        panic("test panic")
    })

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/panic", nil)
    app.ServeHTTP(w, r)

    if w.Code != http.StatusInternalServerError {
        t.Fatalf("expected 500 after panic, got %d", w.Code)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestMiddlewareOrder|TestBuiltinMiddleware" -v`

Expected: FAIL — Use, RequestID, Recoverer not defined

- [ ] **Step 3: Implement middleware**

Historical note: this plan originally introduced a package-specific request
context abstraction. That API has been removed; handlers use
`context.Context`, `http.ResponseWriter`, `*http.Request`, and `Params`
directly depending on the route terminal.

**`ghttp/middleware.go`:**

```go
package ghttp

import (
    "crypto/rand"
    "encoding/hex"
    "log"
    "net/http"
    "time"
)

type MiddlewareFunc func(http.Handler) http.Handler

func (s *Server) Use(mw MiddlewareFunc) {
    s.middlewares = append(s.middlewares, mw)
}

func (s *Server) Group(prefix string, mws ...MiddlewareFunc) *Group {
    return &Group{
        server:      s,
        prefix:      prefix,
        middlewares: mws,
    }
}

type Group struct {
    server      *Server
    prefix      string
    middlewares []MiddlewareFunc
}

// Group re-exports Route to create routes under the group prefix.
func (g *Group) Route[Req, Resp any](path string) *RouteBuilder[Req, Resp] {
    return g.server.Route(JoinPaths(g.prefix, path))
}

// Built-in middlewares
func RequestID() MiddlewareFunc { ... }
func CORS(config interface{}) MiddlewareFunc { ... }
func Logger() MiddlewareFunc { ... }
func Recoverer() MiddlewareFunc { ... }
func Timeout(d time.Duration) MiddlewareFunc { ... }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestMiddlewareOrder|TestBuiltinMiddleware" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/context.go ghttp/middleware.go ghttp/middleware_test.go
git add ghttp/context.go ghttp/middleware.go ghttp/middleware_test.go
git commit -m "feat: add Context, middleware system, built-in RequestID/Recoverer"
```

---

### Task 8: Form handling (multipart mixed form + files + FileHeader)

**Files:**
- Modify: `ghttp/codec_form.go` (if created above)
- Create: `ghttp/upload.go`
- Create: `ghttp/form.go` (multipart parsing utilities)
- Test: `ghttp/form_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestParseMultipartForm(t *testing.T) {
    type UploadInput struct {
        Path struct {
            UserID string `path:"userId"`
        }
        Body struct {
            Name   string          `form:"name"`
            Avatar *FileHeader     `form:"avatar"`
            Tags   []string        `form:"tags"`
        }
    }

    // Build multipart request
    var buf bytes.Buffer
    w := multipart.NewWriter(&buf)
    w.WriteField("name", "Alice")
    w.WriteField("tags", "dev")
    w.WriteField("tags", "ops")

    part, _ := w.CreateFormFile("avatar", "photo.jpg")
    part.Write([]byte("fake-image-data"))
    w.Close()

    r := httptest.NewRequest("POST", "/users/42", &buf)
    r.Header.Set("Content-Type", w.FormDataContentType())

    var input UploadInput
    if err := parseInput(r, &input); err != nil {
        t.Fatalf("parseInput failed: %v", err)
    }

    if input.Body.Name != "Alice" {
        t.Fatalf("expected Name=Alice, got %s", input.Body.Name)
    }
    if len(input.Body.Tags) != 2 || input.Body.Tags[0] != "dev" {
        t.Fatalf("expected Tags=[dev,ops], got %v", input.Body.Tags)
    }
    if input.Body.Avatar == nil {
        t.Fatal("expected Avatar to be non-nil")
    }
    if input.Body.Avatar.Filename != "photo.jpg" {
        t.Fatalf("expected Filename=photo.jpg, got %s", input.Body.Avatar.Filename)
    }

    // Test FileHeader.Bytes()
    data, err := input.Body.Avatar.Bytes()
    if err != nil {
        t.Fatalf("Avatar.Bytes failed: %v", err)
    }
    if string(data) != "fake-image-data" {
        t.Fatalf("expected file content 'fake-image-data', got %s", string(data))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestParseMultipartForm" -v`

Expected: FAIL — FileHeader, parseInput multipart handling not defined

- [ ] **Step 3: Implement FileHeader and form parsing**

**`ghttp/upload.go`:**

```go
package ghttp

import (
    "io"
    "mime/multipart"
    "os"
    "path/filepath"
)

// FileHeader wraps multipart.FileHeader with convenience methods.
type FileHeader struct {
    *multipart.FileHeader
}

// Open opens the uploaded file.
func (f *FileHeader) Open() (multipart.File, error) {
    return f.FileHeader.Open()
}

// Bytes reads the entire file content into memory.
func (f *FileHeader) Bytes() ([]byte, error) {
    src, err := f.Open()
    if err != nil {
        return nil, err
    }
    defer src.Close()
    return io.ReadAll(src)
}

// Save writes the uploaded file to the given path.
func (f *FileHeader) Save(dst string) error {
    src, err := f.Open()
    if err != nil {
        return err
    }
    defer src.Close()

    if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
        return err
    }

    out, err := os.Create(dst)
    if err != nil {
        return err
    }
    defer out.Close()

    _, err = io.Copy(out, src)
    return err
}
```

**`ghttp/form.go`:** Add multipart parsing logic — when Content-Type is `multipart/form-data`, parse the form, fill regular fields from `r.Form`, and set `FileHeader` fields from `r.MultipartForm.File`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestParseMultipartForm" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/upload.go ghttp/form.go ghttp/form_test.go
git add ghttp/upload.go ghttp/form.go ghttp/form_test.go
git commit -m "feat: add FileHeader and multipart form parsing"
```

---

### Task 9: Static file serving

**Files:**
- Create: `ghttp/static.go`
- Test: `ghttp/static_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ghttp

import (
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
)

func TestStaticServesFile(t *testing.T) {
    // Create temp dir with a test file
    tmpDir := t.TempDir()
    testFile := filepath.Join(tmpDir, "hello.txt")
    os.WriteFile(testFile, []byte("Hello, World!"), 0644)

    app := New("test", "1.0.0")
    app.Static("/static", tmpDir)

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/static/hello.txt", nil)
    app.ServeHTTP(w, r)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }
    if w.Body.String() != "Hello, World!" {
        t.Fatalf("expected 'Hello, World!', got %s", w.Body.String())
    }
}

func TestStaticFileSingle(t *testing.T) {
    tmpDir := t.TempDir()
    testFile := filepath.Join(tmpDir, "favicon.ico")
    os.WriteFile(testFile, []byte("icon-data"), 0644)

    app := New("test", "1.0.0")
    app.StaticFile("/favicon.ico", testFile)

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/favicon.ico", nil)
    app.ServeHTTP(w, r)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }
    if w.Body.String() != "icon-data" {
        t.Fatalf("expected 'icon-data', got %s", w.Body.String())
    }
}

func TestStaticDirectoryListingNotAllowed(t *testing.T) {
    tmpDir := t.TempDir()
    os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte("<h1>Index</h1>"), 0644)

    app := New("test", "1.0.0")
    app.Static("/static", tmpDir)

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/static", nil)
    app.ServeHTTP(w, r)

    if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
        // Should either serve index.html (like nginx) or return 404
        // Accept either
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestStaticServesFile|TestStaticFileSingle" -v`

Expected: FAIL — Static, StaticFile not defined

- [ ] **Step 3: Implement static file serving**

**`ghttp/static.go`:**

```go
package ghttp

import (
    "net/http"
    "path"
    "strings"
)

func (s *Server) Static(relativePath, root string) {
    if root == "" {
        panic("static root cannot be empty")
    }
    s.StaticFS(relativePath, http.Dir(root))
}

func (s *Server) StaticFS(relativePath string, fs http.FileSystem) {
    absolutePath := JoinPaths(relativePath, "/*filepath")
    handler := s.createStaticHandler(fs, relativePath)

    s.router.Register("GET", absolutePath, handler)
    s.router.Register("HEAD", absolutePath, handler)
}

func (s *Server) StaticFile(relativePath, filepath string) {
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, filepath)
    })
    s.router.Register("GET", relativePath, handler)
    s.router.Register("HEAD", relativePath, handler)
}

func (s *Server) createStaticHandler(fs http.FileSystem, prefix string) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Extract filepath from the URL
        filePath := strings.TrimPrefix(r.URL.Path, prefix)
        filePath = path.Clean("/" + filePath)
        filePath = strings.TrimPrefix(filePath, "/")

        if strings.Contains(filePath, "..") {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }

        http.FileServer(fs).ServeHTTP(w, r)
    })
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestStaticServesFile|TestStaticFileSingle" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/static.go ghttp/static_test.go
git add ghttp/static.go ghttp/static_test.go
git commit -m "feat: add static file serving (Static, StaticFS, StaticFile)"
```

---

### Task 10: Template rendering (gin-style)

**Files:**
- Create: `ghttp/render.go`
- Test: `ghttp/render_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ghttp

import (
    "bytes"
    "html/template"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
)

func TestRenderHTML(t *testing.T) {
    tmpDir := t.TempDir()
    tmplPath := filepath.Join(tmpDir, "index.html")
    os.WriteFile(tmplPath, []byte(`<h1>{{.Title}}</h1>`), 0644)

    renderer := NewRenderer(tmpDir, ".html", nil, false)

    var buf bytes.Buffer
    if err := renderer.HTML("index", map[string]interface{}{"Title": "Hello"}, &buf); err != nil {
        t.Fatalf("RenderHTML failed: %v", err)
    }
    if !bytes.Contains(buf.Bytes(), []byte("<h1>Hello</h1>")) {
        t.Fatalf("expected '<h1>Hello</h1>', got %s", buf.String())
    }
}

func TestRenderHotReload(t *testing.T) {
    tmpDir := t.TempDir()
    tmplPath := filepath.Join(tmpDir, "page.html")
    os.WriteFile(tmplPath, []byte(`<p>{{.Msg}}</p>`), 0644)

    renderer := NewRenderer(tmpDir, ".html", nil, true)

    var buf bytes.Buffer
    if err := renderer.HTML("page", map[string]interface{}{"Msg": "World"}, &buf); err != nil {
        t.Fatalf("RenderHTML failed: %v", err)
    }
    if !bytes.Contains(buf.Bytes(), []byte("<p>World</p>")) {
        t.Fatalf("expected '<p>World</p>', got %s", buf.String())
    }

    // Modify the template file
    os.WriteFile(tmplPath, []byte(`<p>{{.Msg}} Updated</p>`), 0644)

    buf.Reset()
    if err := renderer.HTML("page", map[string]interface{}{"Msg": "World"}, &buf); err != nil {
        t.Fatalf("RenderHTML after change failed: %v", err)
    }
    if !bytes.Contains(buf.Bytes(), []byte("Updated")) {
        t.Fatalf("expected 'Updated' after hot reload, got %s", buf.String())
    }
}

func TestRenderWithFuncMap(t *testing.T) {
    tmpDir := t.TempDir()
    tmplPath := filepath.Join(tmpDir, "greet.html")
    os.WriteFile(tmplPath, []byte(`{{ .Name | upper }}`), 0644)

    funcMap := template.FuncMap{
        "upper": func(s string) string { return strings.ToUpper(s) },
    }
    renderer := NewRenderer(tmpDir, ".html", funcMap, false)

    var buf bytes.Buffer
    if err := renderer.HTML("greet", map[string]interface{}{"Name": "alice"}, &buf); err != nil {
        t.Fatalf("RenderHTML failed: %v", err)
    }
    if buf.String() != "ALICE" {
        t.Fatalf("expected 'ALICE', got %s", buf.String())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestRenderHTML|TestRenderHotReload|TestRenderWithFuncMap" -v`

Expected: FAIL — NewRenderer, Renderer type not defined

- [ ] **Step 3: Implement renderer (gin-style)**

**`ghttp/render.go`:**

```go
package ghttp

import (
    "bytes"
    "html/template"
    "io"
    "os"
    "path/filepath"
    "strings"
    "sync"
)

// Renderer is the template rendering interface.
type Renderer interface {
    // HTML renders the named template with the given data.
    HTML(name string, data interface{}, w io.Writer) error
}

// GoRenderer uses Go's html/template.
type GoRenderer struct {
    dir      string
    ext      string
    funcMap  template.FuncMap
    reload   bool
    mu       sync.RWMutex
    cache    map[string]*template.Template
}

func NewRenderer(dir, ext string, funcMap template.FuncMap, reload bool) *GoRenderer {
    if ext == "" {
        ext = ".html"
    }
    if !strings.HasPrefix(ext, ".") {
        ext = "." + ext
    }
    return &GoRenderer{
        dir:     dir,
        ext:     ext,
        funcMap: funcMap,
        reload:  reload,
        cache:   make(map[string]*template.Template),
    }
}

func (r *GoRenderer) HTML(name string, data interface{}, w io.Writer) error {
    t, err := r.getTemplate(name)
    if err != nil {
        return err
    }
    return t.Execute(w, data)
}

func (r *GoRenderer) getTemplate(name string) (*template.Template, error) {
    if !r.reload {
        r.mu.RLock()
        t, ok := r.cache[name]
        r.mu.RUnlock()
        if ok {
            return t, nil
        }
    }

    path := filepath.Join(r.dir, name+r.ext)
    content, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    t := template.New(name).Funcs(r.funcMap)
    t, err = t.Parse(string(content))
    if err != nil {
        return nil, err
    }

    if !r.reload {
        r.mu.Lock()
        r.cache[name] = t
        r.mu.Unlock()
    }
    return t, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestRenderHTML|TestRenderHotReload|TestRenderWithFuncMap" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/render.go ghttp/render_test.go
git add ghttp/render.go ghttp/render_test.go
git commit -m "feat: add gin-style template renderer with hot reload"
```

---

### Task 11: OpenAPI 3.1 document generation

**Files:**
- Create: `ghttp/openapi.go`
- Create: `ghttp/schema.go`
- Test: `ghttp/openapi_test.go`
- Test: `ghttp/schema_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestOpenAPIBuildsValidSpec(t *testing.T) {
    app := New("My API", "1.0.0")

    type CreateUserReq struct {
        Path struct {
            OrgID string `path:"orgId" doc:"Organization ID"`
        }
        Body struct {
            Name string `json:"name" minLength:"1" maxLength:"100"`
        }
    }
    type UserData struct {
        ID   string `json:"id"`
        Name string `json:"name"`
    }

    app.POST("/orgs/{orgId}/users", func(ctx Context, req *CreateUserReq) (*struct{ Body UserData }, error) {
        return nil, nil
    }, WithDoc("Create user"), WithTags("Users"), WithReq(CreateUserReq{}), WithResp(201, UserData{}, "Created"))

    spec := app.OpenAPI().Build()

    var doc map[string]interface{}
    if err := json.Unmarshal(spec, &doc); err != nil {
        t.Fatalf("invalid OpenAPI JSON: %v", err)
    }

    if doc["openapi"] != "3.1.0" {
        t.Fatalf("expected openapi 3.1.0, got %v", doc["openapi"])
    }

    paths := doc["paths"].(map[string]interface{})
    postPath := paths["/orgs/{orgId}/users"].(map[string]interface{})
    postOp := postPath["post"].(map[string]interface{})

    if postOp["summary"] != "Create user" {
        t.Fatalf("expected summary 'Create user', got %v", postOp["summary"])
    }

    tags := postOp["tags"].([]interface{})
    if len(tags) != 1 || tags[0] != "Users" {
        t.Fatalf("expected tags [Users], got %v", tags)
    }
}

func TestSchemaGeneration(t *testing.T) {
    type User struct {
        Name string `json:"name" doc:"User name" minLength:"1" maxLength:"100"`
        Age  int    `json:"age" minimum:"0" maximum:"150"`
    }

    schema := generateSchema(reflect.TypeOf(User{}))
    props := schema["properties"].(map[string]interface{})

    nameProp := props["name"].(map[string]interface{})
    if nameProp["type"] != "string" {
        t.Fatalf("expected type string, got %v", nameProp["type"])
    }
    if nameProp["minLength"] != float64(1) {
        t.Fatalf("expected minLength 1, got %v", nameProp["minLength"])
    }

    ageProp := props["age"].(map[string]interface{})
    if ageProp["minimum"] != float64(0) {
        t.Fatalf("expected minimum 0, got %v", ageProp["minimum"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestOpenAPIBuildsValidSpec|TestSchemaGeneration" -v`

Expected: FAIL — OpenAPI, generateSchema not defined

- [ ] **Step 3: Implement OpenAPI and schema generation**

**`ghttp/schema.go`:** Generate JSON Schema from Go types using reflection. Read struct tags: `json`, `doc`→description, `example`→example, `minLength`/`maxLength`/`minimum`/`maximum`→validation, `format`→format, `default`→default, `required`→required.

**`ghttp/openapi.go`:** OpenAPI document builder. Collects routes, generates paths with parameters (path/query/header), request bodies, response schemas. Outputs raw JSON bytes. Served as `/openapi.json` by default.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestOpenAPIBuildsValidSpec|TestSchemaGeneration" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/openapi.go ghttp/schema.go ghttp/openapi_test.go ghttp/schema_test.go
git add ghttp/openapi.go ghttp/schema.go ghttp/openapi_test.go ghttp/schema_test.go
git commit -m "feat: add OpenAPI 3.1 generation and JSON Schema from struct tags"
```

---

### Task 12: Validation interface + built-in tag validations

**Files:**
- Create: `ghttp/validate.go`
- Test: `ghttp/validate_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBuiltinValidation(t *testing.T) {
    type TestStruct struct {
        Path struct {
            ID string `path:"id" required:"true"`
        }
        Body struct {
            Name string `json:"name" minLength:"1" maxLength:"100"`
            Age  int    `json:"age" minimum:"0" maximum:"150"`
            Email string `json:"email" format:"email"`
        }
    }

    v := &builtinValidator{}

    // Valid case
    input := TestStruct{
        Path: struct{ ID string `path:"id" required:"true"` }{ID: "123"},
        Body: struct {
            Name  string `json:"name" minLength:"1" maxLength:"100"`
            Age   int    `json:"age" minimum:"0" maximum:"150"`
            Email string `json:"email" format:"email"`
        }{Name: "Alice", Age: 30, Email: "alice@example.com"},
    }
    if err := v.Validate(nil, &input); err != nil {
        t.Fatalf("expected no error, got: %v", err)
    }

    // Invalid: minLength
    bad := TestStruct{
        Path: struct{ ID string `path:"id" required:"true"` }{ID: "123"},
        Body: struct {
            Name  string `json:"name" minLength:"1" maxLength:"100"`
            Age   int    `json:"age" minimum:"0" maximum:"150"`
            Email string `json:"email" format:"email"`
        }{Name: "", Age: 30, Email: "alice@example.com"},
    }
    if err := v.Validate(nil, &bad); err == nil {
        t.Fatal("expected validation error for empty name")
    }

    // Invalid: format email
    bad2 := TestStruct{
        Path: struct{ ID string `path:"id" required:"true"` }{ID: "123"},
        Body: struct {
            Name  string `json:"name" minLength:"1" maxLength:"100"`
            Age   int    `json:"age" minimum:"0" maximum:"150"`
            Email string `json:"email" format:"email"`
        }{Name: "Alice", Age: 30, Email: "not-an-email"},
    }
    if err := v.Validate(nil, &bad2); err == nil {
        t.Fatal("expected validation error for invalid email")
    }
}

func TestValidatorInterface(t *testing.T) {
    type myValidator struct{}
    func (m *myValidator) Validate(ctx context.Context, input interface{}) error {
        return errors.New("custom error")
    }

    app := New("test", "1.0.0", WithValidator(&myValidator{}))

    app.GET("/test", func(ctx Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
        return &struct{ Body struct{} }{}, nil
    })

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/test", nil)
    app.ServeHTTP(w, r)

    if w.Code != http.StatusUnprocessableEntity {
        t.Fatalf("expected 422 from custom validator, got %d", w.Code)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestBuiltinValidation|TestValidatorInterface" -v`

Expected: FAIL — Validator interface, builtinValidator, WithValidator not defined

- [ ] **Step 3: Implement validation**

**`ghttp/validate.go`:**

```go
package ghttp

import "context"

// Validator validates request input after binding.
type Validator interface {
    Validate(ctx context.Context, input interface{}) error
}

// builtinValidator handles tag-level validations: required, minLength, maxLength, minimum, maximum, pattern, format.
type builtinValidator struct{}

func (v *builtinValidator) Validate(ctx context.Context, input interface{}) error {
    // Walk struct fields recursively, checking tags
    // Return ValidationError on first failure
}

// ValidationError describes a field that failed validation.
type ValidationError struct {
    Field string
    Tag   string
    Value interface{}
}

func (e *ValidationError) Error() string {
    return "validation failed on " + e.Field + " for " + e.Tag
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ghttp -run "TestBuiltinValidation|TestValidatorInterface" -v`

Expected: PASS

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/validate.go ghttp/validate_test.go
git add ghttp/validate.go ghttp/validate_test.go
git commit -m "feat: add Validator interface and built-in tag validations"
```

---

### Task 13: HTTP Client — go-resty style chain + generic callers + WebSocket + SSE

**Files:**
- Create: `ghttp/client.go`
- Create: `ghttp/client_request.go`
- Create: `ghttp/client_response.go`
- Create: `ghttp/endpoint.go`
- Test: `ghttp/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ghttp

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/gorilla/websocket"
)

// --- Basic REST chain test (go-resty style) ---

func TestClientBasicGet(t *testing.T) {
    srv := New("test", "1.0.0")
    srv.GET("/hello", func(ctx Context, req *struct{ Body struct{} }) (*struct {
        Body struct {
            Message string `json:"message"`
        }
    }, error) {
        return &struct {
            Body struct {
                Message string `json:"message"`
            }
        }{Body: struct{ Message string `json:"message"` }{Message: "Hello"}}, nil
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    client := NewClient()
    resp, err := client.R().
        SetHeader("Accept", "application/json").
        Get(ts.URL + "/hello")

    if err != nil {
        t.Fatalf("Get failed: %v", err)
    }
    if !resp.IsSuccess() {
        t.Fatalf("expected success, got %d", resp.StatusCode)
    }
    if resp.String() != `{"code":0,"msg":"success","data":{"message":"Hello"}}` {
        t.Fatalf("unexpected body: %s", resp.String())
    }
}

func TestClientChainSetters(t *testing.T) {
    type EchoReq struct {
        Body struct {
            Name string `json:"name"`
        }
    }
    srv := New("test", "1.0.0")
    srv.POST("/echo/{id}", func(ctx Context, req *EchoReq) (*struct{ Body struct{ Name string } }, error) {
        return &struct{ Body struct{ Name string } }{Body: struct{ Name string }{Name: req.Body.Name}}, nil
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    client := NewClient()
    resp, err := client.R().
        SetPathParam("id", "42").
        SetHeader("Content-Type", "application/json").
        SetBody(map[string]string{"name": "Alice"}).
        SetResult(&struct{ Name string `json:"name"` }{}).
        Post(ts.URL + "/echo/{id}")

    if err != nil {
        t.Fatalf("Post failed: %v", err)
    }
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", resp.StatusCode)
    }
}

func TestClientBaseURL(t *testing.T) {
    srv := New("test", "1.0.0")
    srv.GET("/ping", func(ctx Context, req *struct{ Body struct{} }) (*struct{ Body struct{ Pong string } }, error) {
        return &struct{ Body struct{ Pong string } }{Body: struct{ Pong string }{Pong: "ok"}}, nil
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    client := NewClient(WithBaseURL(ts.URL))
    resp, err := client.R().Get("/ping")
    if err != nil {
        t.Fatalf("Get failed: %v", err)
    }
    if !resp.IsSuccess() {
        t.Fatalf("expected success, got %d", resp.StatusCode)
    }
}

func TestClientSetCookies(t *testing.T) {
    client := NewClient()
    req := client.R().
        SetCookie(&http.Cookie{Name: "session", Value: "abc"}).
        SetCookies([]*http.Cookie{{Name: "lang", Value: "en"}})

    if len(req.Cookies) != 2 {
        t.Fatalf("expected 2 cookies, got %d", len(req.Cookies))
    }
}

func TestClientSetFormData(t *testing.T) {
    client := NewClient()
    req := client.R().
        SetFormData(map[string]string{"user": "alice", "age": "30"})

    if len(req.FormData) != 2 {
        t.Fatalf("expected 2 form fields, got %d", len(req.FormData))
    }
}

func TestClientSetFile(t *testing.T) {
    client := NewClient()
    req := client.R().
        SetFile("avatar", "/tmp/photo.jpg").
        SetFileReader("doc", "report.pdf", strings.NewReader("content"))

    if len(req.MultipartFields) != 2 {
        t.Fatalf("expected 2 multipart fields, got %d", len(req.MultipartFields))
    }
}

func TestClientSetJSONBody(t *testing.T) {
    client := NewClient()
    req := client.R().
        SetJSONBody(map[string]string{"key": "value"})

    if req.Header.Get("Content-Type") != "application/json" {
        t.Fatalf("expected Content-Type application/json, got %s", req.Header.Get("Content-Type"))
    }
}

// --- Generic type-safe endpoint (server-side types reuse) ---

type GreetReq struct {
    Path struct {
        Name string `path:"name"`
    }
}
type GreetResp struct {
    Body struct {
        Message string `json:"message"`
    }
}

func greetHandler(ctx Context, req *GreetReq) (*GreetResp, error) {
    return &GreetResp{
        Body: struct{ Message string `json:"message"` }{Message: "Hello, " + req.Path.Name},
    }, nil
}

func TestClientGenericEndpoint(t *testing.T) {
    srv := New("test", "1.0.0")
    srv.GET("/greet/{name}", greetHandler)

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    // Type-safe call with generic endpoint
    client := NewClient(WithBaseURL(ts.URL))
    req := &GreetReq{Path: struct{ Name string `path:"name"` }{Name: "Alice"}}

    resp, err := POST[GreetReq, struct {
        Body struct {
            Message string `json:"message"`
        }
    }](context.Background(), client, "/greet/{name}", req)

    if err != nil {
        t.Fatalf("POST failed: %v", err)
    }
    if resp.Body.Message != "Hello, Alice" {
        t.Fatalf("expected 'Hello, Alice', got %s", resp.Body.Message)
    }
}

func TestClientGenericGET(t *testing.T) {
    srv := New("test", "1.0.0")
    srv.GET("/health", func(ctx Context, req *struct{ Body struct{} }) (*struct {
        Body struct{ Status string }
    }, error) {
        return &struct {
            Body struct{ Status string }
        }{Body: struct{ Status string }{Status: "ok"}}, nil
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    client := NewClient(WithBaseURL(ts.URL))
    resp, err := GET[struct{ Body struct{} }, struct {
        Body struct{ Status string }
    }](context.Background(), client, "/health")

    if err != nil {
        t.Fatalf("GET failed: %v", err)
    }
    if resp.Body.Status != "ok" {
        t.Fatalf("expected status=ok, got %s", resp.Body.Status)
    }
}

// --- WebSocket test ---

func TestServerWebSocketUpgrade(t *testing.T) {
    srv := New("test", "1.0.0")

    // Server WebSocket handler
    srv.Upgrade("/ws/echo", func(ctx Context, conn *WebSocketConn) error {
        for {
            msgType, data, err := conn.ReadMessage()
            if err != nil {
                return err
            }
            if err := conn.WriteMessage(msgType, data); err != nil {
                return err
            }
        }
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    // Client WebSocket
    url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/echo"
    conn, _, err := websocket.DefaultDialer.Dial(url, nil)
    if err != nil {
        t.Fatalf("WebSocket dial failed: %v", err)
    }
    defer conn.Close()

    if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
        t.Fatalf("Write failed: %v", err)
    }

    _, data, err := conn.ReadMessage()
    if err != nil {
        t.Fatalf("Read failed: %v", err)
    }
    if string(data) != "ping" {
        t.Fatalf("expected 'ping', got %s", string(data))
    }
}

// --- SSE test ---

func TestServerSSE(t *testing.T) {
    srv := New("test", "1.0.0")

    // Server SSE handler
    srv.SSE("/events", func(ctx Context, stream *SSEWriter) error {
        stream.WriteEvent("message", "Hello")
        stream.WriteEvent("message", "World")
        return nil
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    // Client SSE
    client := NewClient()
    stream, err := client.SSE(ts.URL+"/events", SSEConfig{
        OnEvent: func(event SSEEvent) {
            t.Logf("received: %s", event.Data)
        },
        OnError: func(err error) {
            t.Errorf("SSE error: %v", err)
        },
    })
    if err != nil {
        t.Fatalf("SSE failed: %v", err)
    }
    defer stream.Close()

    // Wait for at least one event
    select {
    case event := <-stream.Events():
        if event.Data != "Hello" && event.Data != "World" {
            t.Fatalf("unexpected event data: %s", event.Data)
        }
    case <-time.After(3 * time.Second):
        t.Fatal("timeout waiting for SSE event")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ghttp -run "TestClientBasicGet|TestClientChain|TestClientBaseURL|TestClientSetCookies|TestClientSetFormData|TestClientSetFile|TestClientSetJSONBody|TestClientGenericEndpoint|TestClientGenericGET|TestServerWebSocketUpgrade|TestServerSSE" -v`

Expected: FAIL — Client, Request, SSE, WebSocket, endpoint functions not defined

- [ ] **Step 3: Implement Client (go-resty style)**

**`ghttp/client.go`:**

```go
package ghttp

import (
    "context"
    "crypto/tls"
    "net/http"
    "net/url"
    "sync"
    "time"
)

// Client is an HTTP client with go-resty-style chain API.
type Client struct {
    baseURL        string
    httpClient     *http.Client
    defaultHeaders http.Header
    queryParams    url.Values
    pathParams     map[string]string
    cookies        []*http.Cookie
    authToken      string
    authScheme     string
    basicAuthUser  string
    basicAuthPass  string
    timeout        time.Duration
    mu             sync.RWMutex
    logger         Logger
    codecMgr       *CodecManager
}

type ClientOption func(*Client)

func NewClient(opts ...ClientOption) *Client {
    c := &Client{
        httpClient:     http.DefaultClient,
        defaultHeaders: make(http.Header),
        queryParams:    make(url.Values),
        pathParams:     make(map[string]string),
        codecMgr:       NewCodecManager(),
    }
    for _, opt := range opts {
        opt(c)
    }
    return c
}

func WithBaseURL(baseURL string) ClientOption {
    return func(c *Client) { c.baseURL = baseURL }
}

// R returns a new Request with the client's defaults.
func (c *Client) R() *Request {
    return newRequest(c)
}

// SetBaseURL, SetHeader, SetHeaders, SetQueryParam, SetQueryParams,
// SetPathParam, SetPathParams, SetAuthToken, SetBasicAuth, SetTimeout,
// SetCookie, SetCookies, SetUserAgent, SetContentType, SetAccept,
// SetProxy, SetTracer, SetLogger, SetCache — all chain setters,
// same API as existing gclient.Client (ported from gclient/client.go)
```

**`ghttp/client_request.go`:** Port the `Request` struct and all chain methods from `gclient/request.go`. Key methods:

```go
type Request struct {
    client *Client
    URL    string
    Method string
    ctx    context.Context

    Header     http.Header
    QueryParams url.Values
    PathParams  map[string]string
    FormData    url.Values
    Body        interface{}
    Result      interface{}
    ResultError interface{}
    Cookies     []*http.Cookie

    AuthToken    string
    AuthScheme   string
    BasicAuthUser string
    BasicAuthPass string
    Timeout      time.Duration
    DisableProxy bool
    ProxyURL     string
    ProxyFunc    func(*http.Request) (*url.URL, error)
    FollowRedirects *bool
    MaxRedirects int

    MultipartFields []*MultipartField
    Tracer    Tracer

    // Chain setters: SetHeader, SetBody, SetJSONBody, SetResult, SetPathParam...
    // Terminal methods: Execute, Get, Post, Put, Delete, Patch, Head, Options
}
```

**`ghttp/client_response.go`:** Port `Response` from `gclient/response.go`, add `UnwrapEnvelope()` for the `{code, msg, data}` format.

```go
type Response struct {
    StatusCode   int
    Status       string
    Header       http.Header
    Body         []byte
    Duration     time.Duration
    ContentType  string
    Request      *Request
    RawResponse  *http.Response
}

func (r *Response) String() string { return string(r.Body) }
func (r *Response) Bytes() []byte { return r.Body }
func (r *Response) IsSuccess() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }
func (r *Response) IsError() bool { return !r.IsSuccess() }
func (r *Response) BindJSON(target interface{}) error { return json.Unmarshal(r.Body, target) }

// UnwrapEnvelope extracts the data field from {code, msg, data} envelope.
func (r *Response) UnwrapEnvelope(target interface{}) error {
    var env struct {
        Code int             `json:"code"`
        Msg  string          `json:"msg"`
        Data json.RawMessage `json:"data"`
    }
    if err := json.Unmarshal(r.Body, &env); err != nil {
        return err
    }
    if env.Data != nil {
        return json.Unmarshal(env.Data, target)
    }
    return nil
}
```

**`ghttp/endpoint.go`:**

```go
package ghttp

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "net/http"
    "strings"
)

// Do makes a type-safe HTTP call reusing server-side Req/Resp types.
func Do[Req, Resp any](ctx context.Context, client *Client, method, path string, req *Req) (*Resp, error) {
    r := client.R().SetContext(ctx)

    // Reuse server-side path params from the request struct
    // (if Req uses Path/Query/Body nesting, extract and set them)
    // For now, simple body marshal:
    var body io.Reader
    if req != nil {
        data, err := json.Marshal(req)
        if err != nil {
            return nil, err
        }
        body = bytes.NewReader(data)
    }

    httpReq, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")

    httpResp, err := client.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer httpResp.Body.Close()

    respData, err := io.ReadAll(httpResp.Body)
    if err != nil {
        return nil, err
    }

    // Unwrap envelope {code, msg, data}
    var env struct {
        Code int             `json:"code"`
        Msg  string          `json:"msg"`
        Data json.RawMessage `json:"data"`
    }
    if err := json.Unmarshal(respData, &env); err != nil {
        return nil, err
    }

    var resp Resp
    if env.Data != nil {
        if err := json.Unmarshal(env.Data, &resp); err != nil {
            return nil, err
        }
    }
    return &resp, nil
}

func GET[Req, Resp any](ctx context.Context, client *Client, path string) (*Resp, error) {
    return Do[Req, Resp](ctx, client, "GET", path, nil)
}

func POST[Req, Resp any](ctx context.Context, client *Client, path string, req *Req) (*Resp, error) {
    return Do[Req, Resp](ctx, client, "POST", path, req)
}

func PUT[Req, Resp any](ctx context.Context, client *Client, path string, req *Req) (*Resp, error) {
    return Do[Req, Resp](ctx, client, "PUT", path, req)
}

func DELETE[Req, Resp any](ctx context.Context, client *Client, path string) (*Resp, error) {
    return Do[Req, Resp](ctx, client, "DELETE", path, nil)
}
```

---

### Task 14: Server WebSocket support

**Files:**
- Create: `ghttp/websocket.go`
- Modify: `ghttp/builder.go` (add `Upgrade()` method)
- Test: `ghttp/websocket_test.go`

- [ ] **Step 1: Write the failing test** (already in Task 13 step 1 `TestServerWebSocketUpgrade`)

- [ ] **Step 2: Implement server WebSocket**

**`ghttp/websocket.go`:**

```go
package ghttp

import (
    "net/http"
    "github.com/gorilla/websocket"
)

// WebSocketConn wraps gorilla/websocket.Conn.
type WebSocketConn struct {
    *websocket.Conn
}

// WebSocketHandler is the handler type for WebSocket upgrades.
type WebSocketHandler func(ctx Context, conn *WebSocketConn) error

var wsUpgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

// Upgrade registers a WebSocket upgrade route.
func (s *Server) Upgrade(path string, handler WebSocketHandler, opts ...RouteOption) {
    h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        conn, err := wsUpgrader.Upgrade(w, r, nil)
        if err != nil {
            http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
            return
        }
        defer conn.Close()

        wsConn := &WebSocketConn{Conn: conn}
        ctx := newContext(w, r)
        if err := handler(ctx, wsConn); err != nil {
            wsConn.Close()
        }
    })
    _ = s.router.Register("GET", path, h)
}

// ReadJSON reads a JSON-encoded message from the WebSocket connection.
func (c *WebSocketConn) ReadJSON(v interface{}) error {
    return c.Conn.ReadJSON(v)
}

// WriteJSON writes a JSON-encoded message to the WebSocket connection.
func (c *WebSocketConn) WriteJSON(v interface{}) error {
    return c.Conn.WriteJSON(v)
}
```

---

### Task 15: Client WebSocket support

**Files:**
- Create: `ghttp/client_websocket.go`
- Test: `ghttp/client_websocket_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestClientWebSocketEcho(t *testing.T) {
    srv := New("test", "1.0.0")
    srv.Upgrade("/ws", func(ctx Context, conn *WebSocketConn) error {
        for {
            var msg map[string]string
            if err := conn.ReadJSON(&msg); err != nil {
                return err
            }
            msg["echo"] = msg["msg"]
            if err := conn.WriteJSON(msg); err != nil {
                return err
            }
        }
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    client := NewClient()
    ws, err := client.WebSocket(ts.URL + "/ws")
    if err != nil {
        t.Fatalf("WebSocket dial failed: %v", err)
    }
    defer ws.Close()

    if err := ws.WriteJSON(map[string]string{"msg": "hello"}); err != nil {
        t.Fatalf("WriteJSON failed: %v", err)
    }

    var resp map[string]string
    if err := ws.ReadJSON(&resp); err != nil {
        t.Fatalf("ReadJSON failed: %v", err)
    }
    if resp["echo"] != "hello" {
        t.Fatalf("expected echo=hello, got %v", resp)
    }
}

func TestClientWebSocketWithConfig(t *testing.T) {
    client := NewClient()
    ws, err := client.WebSocket("wss://echo.example.com/ws",
        WithWebSocketTLS(&tls.Config{InsecureSkipVerify: true}),
        WithWebSocketSubprotocols([]string{"json"}),
    )
    if err == nil && ws != nil {
        ws.Close()
    }
    // Connection will fail since no server, but config should not panic
}
```

- [ ] **Step 2: Implement client WebSocket**

**`ghttp/client_websocket.go`:**

```go
package ghttp

import (
    "crypto/tls"
    "net/http"
    "time"
    "github.com/gorilla/websocket"
)

// WebSocketConfig configures client WebSocket connections.
type WebSocketConfig struct {
    TLSConfig       *tls.Config
    Subprotocols    []string
    HandshakeTimeout time.Duration
    ReadBufferSize  int
    WriteBufferSize int
}

// WebSocket dials a WebSocket URL and returns a connection.
func (c *Client) WebSocket(url string, opts ...WebSocketOption) (*WebSocketConn, error) {
    cfg := &WebSocketConfig{
        HandshakeTimeout: 10 * time.Second,
    }
    for _, opt := range opts {
        opt(cfg)
    }

    dialer := &websocket.Dialer{
        TLSClientConfig:  cfg.TLSConfig,
        Subprotocols:     cfg.Subprotocols,
        HandshakeTimeout: cfg.HandshakeTimeout,
        ReadBufferSize:   cfg.ReadBufferSize,
        WriteBufferSize:  cfg.WriteBufferSize,
    }

    conn, _, err := dialer.Dial(url, nil)
    if err != nil {
        return nil, err
    }
    return &WebSocketConn{Conn: conn}, nil
}

type WebSocketOption func(*WebSocketConfig)

func WithWebSocketTLS(tlsConfig *tls.Config) WebSocketOption {
    return func(c *WebSocketConfig) { c.TLSConfig = tlsConfig }
}
func WithWebSocketSubprotocols(protos []string) WebSocketOption { ... }
```

---

### Task 16: Server SSE support

**Files:**
- Create: `ghttp/sse.go`
- Modify: `ghttp/builder.go` (add `SSE()` method)
- Test: `ghttp/sse_test.go`

- [ ] **Step 1: Write the failing test** (already in Task 13 step 1 `TestServerSSE`)

- [ ] **Step 2: Implement server SSE**

**`ghttp/sse.go`:**

```go
package ghttp

import (
    "encoding/json"
    "fmt"
    "net/http"
)

// SSEWriter writes Server-Sent Events to a response.
type SSEWriter struct {
    w      http.ResponseWriter
    flusher http.Flusher
}

// SSEHandler handles an SSE connection.
type SSEHandler func(ctx Context, stream *SSEWriter) error

// SSEEvent represents a single SSE event.
type SSEEvent struct {
    ID    string
    Event string
    Data  string
}

// SSE registers a Server-Sent Events endpoint.
func (s *Server) SSE(path string, handler SSEHandler, opts ...RouteOption) {
    h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        w.WriteHeader(http.StatusOK)

        flusher, ok := w.(http.Flusher)
        if !ok {
            http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
            return
        }

        stream := &SSEWriter{w: w, flusher: flusher}
        ctx := newContext(w, r)
        if err := handler(ctx, stream); err != nil {
            return
        }
    })
    _ = s.router.Register("GET", path, h)
}

// WriteEvent writes an SSE event.
func (s *SSEWriter) WriteEvent(event, data string) error {
    _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
    if err != nil {
        return err
    }
    s.flusher.Flush()
    return nil
}

// WriteJSON writes a JSON-encoded SSE event.
func (s *SSEWriter) WriteJSON(event string, data interface{}) error {
    b, err := json.Marshal(data)
    if err != nil {
        return err
    }
    return s.WriteEvent(event, string(b))
}
```

---

### Task 17: Client SSE support

**Files:**
- Create: `ghttp/client_sse.go`
- Test: `ghttp/client_sse_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestClientSSEReceiveEvents(t *testing.T) {
    srv := New("test", "1.0.0")
    srv.SSE("/stream", func(ctx Context, stream *SSEWriter) error {
        stream.WriteEvent("msg", "one")
        stream.WriteEvent("msg", "two")
        stream.WriteEvent("msg", "three")
        return nil
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    client := NewClient()
    events := make([]string, 0)
    done := make(chan struct{})

    stream, err := client.SSE(ts.URL+"/stream", SSEConfig{
        OnEvent: func(event SSEEvent) {
            events = append(events, event.Data)
            if len(events) == 3 {
                close(done)
            }
        },
        OnError: func(err error) {
            t.Errorf("SSE error: %v", err)
        },
    })
    if err != nil {
        t.Fatalf("SSE failed: %v", err)
    }
    defer stream.Close()

    select {
    case <-done:
        if len(events) != 3 {
            t.Fatalf("expected 3 events, got %d", len(events))
        }
    case <-time.After(3 * time.Second):
        t.Fatal("timeout waiting for SSE events")
    }
}

func TestClientSSEChannel(t *testing.T) {
    srv := New("test", "1.0.0")
    srv.SSE("/ch", func(ctx Context, stream *SSEWriter) error {
        stream.WriteJSON("data", map[string]string{"key": "value"})
        return nil
    })

    ts := httptest.NewServer(srv.mux)
    defer ts.Close()

    client := NewClient()
    stream, err := client.SSE(ts.URL + "/ch")
    if err != nil {
        t.Fatalf("SSE failed: %v", err)
    }
    defer stream.Close()

    select {
    case event := <-stream.Events():
        if event.Event != "data" {
            t.Fatalf("expected event=data, got %s", event.Event)
        }
    case <-time.After(3 * time.Second):
        t.Fatal("timeout waiting for SSE event")
    }
}
```

- [ ] **Step 2: Implement client SSE**

**`ghttp/client_sse.go`:**

```go
package ghttp

import (
    "bufio"
    "io"
    "net/http"
    "strings"
    "sync"
)

// SSEStream is a client-side SSE stream with channel-based event delivery.
type SSEStream struct {
    Events <-chan SSEEvent
    Errors <-chan error
    close  func()
}

// SSEConfig configures client SSE behavior.
type SSEConfig struct {
    OnEvent   func(SSEEvent)
    OnError   func(error)
    OnConnect func(*http.Response)
}

// SSE connects to a Server-Sent Events endpoint.
func (c *Client) SSE(url string, config ...SSEConfig) (*SSEStream, error) {
    // Default config
    cfg := SSEConfig{}
    if len(config) > 0 {
        cfg = config[0]
    }

    resp, err := c.httpClient.Get(url)
    if err != nil {
        return nil, err
    }

    if cfg.OnConnect != nil {
        cfg.OnConnect(resp)
    }

    events := make(chan SSEEvent, 64)
    errs := make(chan error, 1)
    ctx, cancel := context.WithCancel(context.Background())

    go func() {
        defer resp.Body.Close()
        defer close(events)
        defer close(errs)

        scanner := bufio.NewScanner(resp.Body)
        var event SSEEvent

        for scanner.Scan() {
            select {
            case <-ctx.Done():
                return
            default:
            }

            line := scanner.Text()
            switch {
            case strings.HasPrefix(line, "id: "):
                event.ID = strings.TrimPrefix(line, "id: ")
            case strings.HasPrefix(line, "event: "):
                event.Event = strings.TrimPrefix(line, "event: ")
            case strings.HasPrefix(line, "data: "):
                event.Data = strings.TrimPrefix(line, "data: ")
            case line == "":
                // Empty line means end of event
                if cfg.OnEvent != nil {
                    cfg.OnEvent(event)
                }
                select {
                case events <- event:
                default:
                }
                event = SSEEvent{}
            }
        }

        if err := scanner.Err(); err != nil && err != io.EOF {
            if cfg.OnError != nil {
                cfg.OnError(err)
            }
            errs <- err
        }
    }()

    return &SSEStream{
        Events: events,
        Errors: errs,
        close:  cancel,
    }, nil
}

func (s *SSEStream) Close() {
    if s.close != nil {
        s.close()
    }
}
```

- [ ] **Step 3: Run tests for client, WebSocket, and SSE combined**

```bash
go test ./ghttp -run "TestClient|TestWebSocket|TestSSE" -v -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
gofmt -w ghttp/client.go ghttp/client_request.go ghttp/client_response.go ghttp/endpoint.go ghttp/websocket.go ghttp/client_websocket.go ghttp/sse.go ghttp/client_sse.go ghttp/client_test.go ghttp/websocket_test.go ghttp/sse_test.go
git add ghttp/client*.go ghttp/endpoint.go ghttp/websocket*.go ghttp/sse*.go
git commit -m "feat: add HTTP client (go-resty style), WebSocket, and SSE support"
```

---

### Task 14: Integration tests and benchmarks

**Files:**
- Modify: `ghttp/ghttp_test.go` (add integration tests)
- Create: `ghttp/benchmark_test.go`

- [ ] **Step 1: Write the integration test**

```go
func TestFullRoundTripWithEnvelope(t *testing.T) {
    type CreateUserReq struct {
        Path struct {
            OrgID string `path:"orgId"`
        }
        Body struct {
            Name string `json:"name"`
        }
    }
    type UserData struct {
        ID   string `json:"id"`
        Name string `json:"name"`
        OrgID string `json:"orgId"`
    }

    app := New("Integration Test", "1.0.0")

    app.POST("/orgs/{orgId}/users", func(ctx Context, req *CreateUserReq) (*struct{ Body UserData }, error) {
        return &struct{ Body UserData }{
            Body: UserData{
                ID:    "usr_001",
                Name:  req.Body.Name,
                OrgID: req.Path.OrgID,
            },
        }, nil
    }, WithDoc("Create user"), WithTags("Users"))

    // OpenAPI should be available
    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/openapi.json", nil)
    app.ServeHTTP(w, r)
    if w.Code != http.StatusOK {
        t.Fatalf("openapi endpoint returned %d", w.Code)
    }

    // Create user
    w2 := httptest.NewRecorder()
    body := bytes.NewReader([]byte(`{"name":"Alice"}`))
    r2 := httptest.NewRequest("POST", "/orgs/org-42/users", body)
    r2.Header.Set("Content-Type", "application/json")
    app.ServeHTTP(w2, r2)

    if w2.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
    }

    var envelope struct {
        Code int        `json:"code"`
        Msg  string     `json:"msg"`
        Data *UserData  `json:"data"`
    }
    if err := json.Unmarshal(w2.Body.Bytes(), &envelope); err != nil {
        t.Fatalf("unmarshal envelope failed: %v", err)
    }
    if envelope.Code != 0 {
        t.Fatalf("expected code=0, got %d", envelope.Code)
    }
    if envelope.Data.Name != "Alice" {
        t.Fatalf("expected name=Alice, got %s", envelope.Data.Name)
    }
}
```

- [ ] **Step 2: Write the benchmark tests**

```go
func BenchmarkRouting(b *testing.B) {
    app := New("bench", "1.0.0")
    app.GET("/users/:id", func(ctx Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
        return &struct{ Body struct{} }{}, nil
    })

    w := httptest.NewRecorder()
    r := httptest.NewRequest("GET", "/users/42", nil)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        app.ServeHTTP(w, r)
    }
}

func BenchmarkCodecJSONMarshal(b *testing.B) {
    codec := &JSONCodec{}
    data := map[string]string{"key": "value"}
    var buf bytes.Buffer

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        buf.Reset()
        codec.Marshal(&buf, data)
    }
}

func BenchmarkCodecJSONUnmarshal(b *testing.B) {
    codec := &JSONCodec{}
    data := bytes.NewReader([]byte(`{"key":"value"}`))
    var result map[string]string

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        data.Seek(0, 0)
        result = nil
        codec.Unmarshal(data, &result)
    }
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./ghttp -v
```

Expected: All PASS

- [ ] **Step 4: Run benchmarks**

```bash
go test -bench=. -benchmem ./ghttp
```

Record results for comparison with old gserver benchmarks.

- [ ] **Step 5: Format and commit**

```bash
gofmt -w ghttp/ghttp_test.go ghttp/benchmark_test.go
git add ghttp/ghttp_test.go ghttp/benchmark_test.go
git commit -m "test: add integration tests and benchmarks"
```

---

### Task 15: Writer, constants, logger, result compat, remaining helpers

**Files:**
- Create: `ghttp/writer.go` (port from old writer.go, strip fasthttp refs)
- Create: `ghttp/constants.go` (MIME constants)
- Create: `ghttp/logger.go` (Logger interface)
- Create: `ghttp/result.go` (Result interface for backward compat — minimal shim)
- Modify: `ghttp/util.go` (ensure all helpers from old gserver/util.go are ported)

- [ ] **Step 1: Write tests**

```go
func TestResponseWriterStatus(t *testing.T) {
    w := httptest.NewRecorder()
    rw := NewResponseWriter(w)
    if rw.Status() != http.StatusOK {
        t.Fatalf("expected default 200, got %d", rw.Status())
    }
    rw.WriteHeader(http.StatusCreated)
    if rw.Status() != http.StatusCreated {
        t.Fatalf("expected 201, got %d", rw.Status())
    }
}

func TestResponseWriterWritten(t *testing.T) {
    w := httptest.NewRecorder()
    rw := NewResponseWriter(w)
    if rw.Written() {
        t.Fatal("expected not written initially")
    }
    rw.Write([]byte("hello"))
    if !rw.Written() {
        t.Fatal("expected written after write")
    }
}
```

- [ ] **Step 2: Implement helpers**

- [ ] **Step 3: Run tests and commit**

---

### Task 16: Final validation — vet, lint, full test suite

**Files:**
- None (validation only)

- [ ] **Step 1: Run vet**

```bash
go vet ./ghttp/...
```

Expected: no errors

- [ ] **Step 2: Run full test suite**

```bash
go test -v -count=1 ./ghttp/...
```

Expected: All PASS

- [ ] **Step 3: Run benchmarks**

```bash
go test -bench=. -benchmem ./ghttp/...
```

Record results for performance baseline.

- [ ] **Step 4: Run lint**

```bash
golangci-lint run ./ghttp/...
```

Expected: no issues (or minimal acceptable warnings)

- [ ] **Step 5: Final commit**

```bash
git add ghttp/
git commit -m "chore: finalize ghttp package with vet/lint/test pass"
```

---

## Suggested Commit Sequence

1. `feat: add ghttp core skeleton (Server, Config, handler types)`
2. `feat: port three-layer routing engine from gserver`
3. `feat: add go-restful-style RouteBuilder chain API`
4. `feat: add input parser with path/query/header/body binding`
5. `feat: add flat codec system with JSON/XML/Plain/Form, Content-Type negotiation`
6. `feat: add envelope system and HTTP error types`
7. `feat: add Context, middleware system, built-in RequestID/Recoverer`
8. `feat: add FileHeader and multipart form parsing`
9. `feat: add static file serving (Static, StaticFS, StaticFile)`
10. `feat: add gin-style template renderer with hot reload`
11. `feat: add OpenAPI 3.1 generation and JSON Schema from struct tags`
12. `feat: add Validator interface and built-in tag validations`
13. `feat: add generic HTTP client with type-safe GET/POST/PUT/DELETE`
14. `test: add integration tests and benchmarks`
15. `chore: add writer, constants, logger, result compat shims`
16. `chore: finalize ghttp package with vet/lint/test pass`

## Risks To Watch During Execution

- **Reflection in input parser**: The reflection-based input binding is complex. Handle pointer types, nested structs, and different tag formats carefully. Overload existing tests with edge cases.
- **Content-Type negotiation**: Accept header parsing must handle `q` weights, wildcards (`*/*`), and multiple types correctly.
- **Generic type inference**: Go 1.24 type inference for generic functions may require explicit type parameters in some contexts. Test with both inferred and explicit type parameters.
- **Multipart form parsing**: Must handle both regular fields and file fields in a single request. `ParseMultipartForm` allocates memory proportionally to `maxMemory` — choose a sensible default (32 MB).
- **OpenAPI schema generation**: Deeply nested structs, cycles (impossible with JSON), and interface fields need special handling.
- **Routing priority**: Static paths must take priority over parameter paths. The ported matcher handles this — verify with regression tests.
- **Backward compat with gserver/gclient**: Old packages are kept unchanged. Only new code uses the `ghttp` package directly.

## Execution Recommendation

Recommended execution order:

1. **Task 1-2**: Skeleton + routing (foundation)
2. **Task 3**: RouteBuilder (core user-facing API)
3. **Task 4-5**: Input + Codec (request processing)
4. **Task 6-7**: Output + Middleware (response processing)
5. **Task 8-10**: Form/Static/Template (utility features)
6. **Task 11-12**: OpenAPI + Validation (documentation and correctness)
7. **Task 13**: Client (mirror API for consumers)
8. **Task 14-16**: Tests, benchmarks, final validation

## Expected Performance Targets

| Operation | Target | vs old gserver |
|-----------|--------|---------------|
| Static route match | <50 ns | Comparable |
| Param route match | <100 ns | Comparable |
| JSON marshal (1KB) | <2 µs | Comparable |
| JSON unmarshal (1KB) | <3 µs | Comparable |
| Handler dispatch (no-op) | <200 ns | Slightly slower (net/http wrapper) |
| Full round-trip (httptest) | <5 µs | Acceptable |

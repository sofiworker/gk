# ghttp v2

[中文](README.md) · [Design (Chinese)](DESIGN.md)

Runnable complete example: [HTTP example](examples/http/README.en.md), with [source](examples/http/main.go).

Experimental, pre-v1, not for production. This separate package is the future HTTP mainline; it does not provide a v1 API compatibility layer.

Generic Get/Head/Post/Put/Patch/Delete/Options/Method constructors infer input and output types. All four independent value/pointer combinations work. Route.Err reports configuration errors; Route.Mount bridges to existing ghttp Server/Group routing and middleware. HEAD suppresses the body.

Default input is JSON unless source tags are present. Registration compiles path/query/header/cookie and one body field (json/xml/form/text). Explicit WithInput replaces the entire binding contract. WithOutput selects an encoder. Codec types must match handler types. CustomInput/CustomOutput expose small Decoder/Encoder interfaces.

Present but mismatched Content-Type produces 415; omitted Content-Type is currently accepted. Decode failures produce 400 and size-limit errors produce 413. The default endpoint body limit is 32 MiB, configurable with WithBodyLimit. Reply[T] carries status, headers, cookies and a body; default JSON output unwraps it. ReplyOutput selects a custom body encoder.

MultipartInput binds form fields, one *multipart.FileHeader or multiple []*multipart.FileHeader. Defaults are a 32 MiB body and 1 MiB file memory; MultipartLimits overrides them. The endpoint body limit also applies. Handlers open and close files; the framework removes temporary files after endpoint completion, including errors. Copy uploads during the request if they must outlive it. Missing files stay nil; multiple files bound to a single-file field are rejected.

No code generation or reflect.Call is used. Source tags are compiled at registration, but runtime field access, form/multipart binding and standard codecs may use reflection. Benchmarks include httptest recorder overhead.

Explicit Accept negotiation, validators, nested source tags, streaming uploads and registration metadata export are available. Batch mounting is not transactional; pointer fields represent presence and business validation is explicit. HTML output expects rendered content; applications must escape untrusted values.

Run go test ./ghttp/v2 and go test -race ./ghttp/v2.

## Groups

Groups provide registration-time prefixes and defaults while reusing the existing ghttp router:

```go
api := v2.New()
users := api.Group("/users", v2.GroupInput(v2.JSONInput[*UserInput]()),
    v2.GroupOutput(v2.JSONOutput[*UserView]()),
).Use(authenticate)
route, err := v2.GetIn(users, server, "/{username}", getUser)
```

Nested prefixes compose and middleware runs parent to child. GroupInput/GroupOutput are typed defaults and must match a route handler; route options are applied later. Routes can prefix and mount a batch, but mounting is not transactional and earlier routes remain installed if a later one fails.

## Recommended registration

Bind the destination once with `api := v2.New(server)`, create groups, then call `users.Register(v2.Get("/{username}", getUser), v2.Post("", createUser))`.

API/Group.With accepts route Options. Register recompiles typed executors with root, parent, child and route options in that order. Middleware appends; codecs and limits override. Installed routes are snapshots; definitions may be reused across groups. Batch configuration and exact duplicate checks precede installation, but backend failures can leave partially installed routes.

Experimental behavior change: Group.Mount/Routes now apply all inherited options, not just middleware. Standalone Route.Serve/Err remain supported; registration performs additional compilation whose cost has not yet been remeasured.

## Input execution

Registration selects a typed decode(ctx, req) function returning I. Each request gets independent input; handlers are called directly. Source-field scalar converters are selected at registration and unsupported types fail early, while field assignment still uses reflection.

WithInput(DecodeWith(fn)) accepts func(context.Context, *Request) (I, error). Custom functions own allocation and nil semantics; they must not share mutable request input. Body limits and input error classification remain outside the decoder. DecodeWith declares no media type; custom protocol validation belongs to the function. Existing Decoder extensions remain supported. Form/multipart plans are constructed at registration; field assignment and standard JSON/XML reflection remain.

For a few query fields in DecodeWith, prefer req.QueryFirst(key), returning value and presence, or QueryValues(key) for repeated values. These reuse lazy parsing with cached-map fallback for large queries. Use req.Query for the complete map; never cache request values across requests.

Handlers may accept `RequestInput` or `*RequestInput` when no domain DTO is needed. Its `Sources()` view reads path, query, header and cookie values from the same request. The view and underlying pooled request are valid only during the request; do not retain them for asynchronous work. Value inputs are constructed directly by the registered reader, without a temporary decoder destination.

## HTTP mainline

`NewServer` owns a private HTTP backend and exposes Register, Group, With, Use, ServeHTTP, Run/RunTLS, Serve/ServeTLS, Shutdown and Close. Configure and register before serving. This is the future v2 mainline, not a v1 API compatibility layer. Batch configuration errors are preflighted; backend installation failures can leave earlier routes installed.

`WithValidator(func(context.Context, T) error)` validates decoded input before handler execution. Group defaults may be overridden per route. `StrictJSONInput[T]` rejects unknown fields and multiple JSON values. Scalar pointer fields distinguish absent input from an explicitly supplied value.

`WithNegotiation(JSONOutput[T](), XMLOutput[T]())` selects outputs using Accept quality, specificity and media parameters, adds Vary: Accept, defaults to the first representation when Accept is absent, and returns 406 when none is acceptable.

`FileReply` delegates seekable file content to http.ServeContent, including Range, conditional requests and HEAD. `StreamReply` copies a reader; supplying Request skips consumption on HEAD. `RedirectReply` returns Location and a redirect status. Supply Closer for automatic closing after encoding, including errors and HEAD. Do not defer Close in the handler: encoding happens after it returns. Without Closer, ownership remains with the caller.

`MultipartStreamInput()` supplies a *multipart.Reader for part-by-part uploads under the endpoint body limit. Handlers consume and close parts. Endpoint cleanup also covers multipart parsed by custom decoders and RequestInput handlers, including errors and panic unwinding.

`WithoutBodyLimit`, `WithoutContentTypeCheck` and `WithoutMultipartCleanup` transfer individual responsibilities to callers; codec constraints remain active. `WithUnsafeFastPath` combines them without Go unsafe. A later WithBodyLimit restores endpoint enforcement. Benchmarks with checks disabled are a different workload.

`server.OpenAPI(title, version)` explicitly exports OpenAPI 3.1 JSON for successfully registered routes, including group prefixes and statically inferable schemas/statuses. Dynamic decoders, XML/multipart or negotiated outputs that cannot be described accurately carry schema-unavailable extensions. It is not a complete business validation-schema generator.

Return `HTTPError{Status: 400, Cause: err}` for manual parsing failures while preserving the error chain. Invalid error statuses become 500. `Recovery()` sends panics through the common error pipeline. See HTTP_MAINLINE.md for implementation boundaries.

## Direct request/response handlers

`Raw(method, path, func(context.Context, *Request, *Response) error, opts...)` returns a regular Route for Server/Group.Register. Return nil on success; errors use the shared error pipeline, without appending an error body after a response is committed. No automatic encoding or 204 is applied. Middleware, body limits, multipart cleanup and HEAD body suppression remain active. Group codec/validator/negotiation defaults do not apply; configuring these explicitly on Raw is a registration error. Handlers own input protocol validation.

# ghttp v3

[简体中文](README.md)

Experimental pre-v1 API, prohibited for direct production use. v3 is a separate package; v2 remains available. This is not a compatible replacement or a frozen API.

## Unified requests

HTTP constructors accept `func(context.Context, RequestOf[T]) (O, error)`. Their generic parameters are data T and output O, usually inferred from the handler. RequestOf[T] combines on-demand request access with Data; JSON, form, multipart and XML share the wrapper.

```go
func updateUser(ctx context.Context, req httpv3.RequestOf[*UpdateBody]) (User, error) {
    notify, _ := req.QueryFirst("notify")
    return service.Update(ctx, req.Path("id"), req.Data, notify == "true")
}

server := httpv3.NewServer()
err := server.Group("/api").Register(
    httpv3.Patch("/users/{id}", updateUser),
)
```

Get/Head/Post/Put/Patch/Delete/Options/Method use the same signature. Ordinary endpoints do not accept arbitrary I or pointer request wrappers; Data itself may be a value or pointer.

## Data codecs

Default input decodes the body as JSON without implicitly binding path/query tags. Request sources are read on demand using Path, QueryFirst/QueryValues, HeaderValue and CookieValue. String accessors do not convert business types. Unused invalid business parameters are not automatically rejected; handlers or explicit binding own conversions. Routing and path capture still occur before the handler.

RequestOf[NoData] explicitly skips default decoding, leaving the body unread. Do not substitute struct{} for NoData. Explicit WithInput can override its default. FromFunc/FromProcedure remain available for handlers needing no request.

WithInput targets T, not RequestOf[T]:

```go
httpv3.Post("/users/{id}/profile", updateProfile,
    httpv3.WithInput(httpv3.FormInput[*ProfileForm]()),
)
httpv3.Post("/users/{id}/attachments", upload,
    httpv3.WithInput(httpv3.MultipartInput[*UploadForm]()),
)
```

XMLInput, TextInput, StrictJSONInput, MultipartStreamInput, CustomInput and DecodeWith also decode Data. Custom decoders return T; the framework constructs the request wrapper. Do not wrap codecs with DecodeRequest in WithInput. Mismatched types fail construction/registration. Group defaults target T and require matching data types; route options override them.

BindInput[T]() explicitly enables path/query/header/cookie tags and at most one tagged body, including nested fields. Undeclared fields remain zero; it never silently falls back to whole-body JSON. Automatic binding uses reflective field assignment. Form/multipart and standard JSON/XML codecs may also use reflection; typed wrappers do not imply zero request-time reflection.

## Validation and lifetime

Default JSON permits absent bodies; pointer Data remains nil for absence or JSON null. An empty reader's EOF is a codec error. RequireBody(codec) requires at least one byte, mapping absence/empty input to 400 with ErrMissingBody; null is not absence. Business field types own absent/null/empty distinctions.

Execution order is Data decoding, WithDataValidator(T), WithValidator(RequestOf[T]), handler. Validators must handle nil pointer Data when allowed. Input validation maps to 400. Authorization belongs in middleware or the handler with an appropriate HTTPError, rather than pure input validation.

For deferred decoding, use RequestOf[NoData] and `ReadBody(ctx, req.RequestInput, codec)`. It checks media types and preserves input error causes, but does not automatically run validators. Bodies are consumed once without automatic caching or replay; callers must restore bodies read by middleware.

The default endpoint body limit is 32 MiB, configurable with WithBodyLimit. Malformed input maps to 400, excess size to 413 and declared media mismatch to 415; omitted Content-Type remains permitted. MultipartInput has independent body/memory limits and temporary files are cleaned up when the endpoint returns. Handlers close opened files and multipart parts. Request views, uploaded files and streaming readers cannot outlive the request; copy data for asynchronous work.

## Middleware and output

Middleware remains `func(Handler) Handler`, where Handler is `func(context.Context, *Request, *Response) error`, independent of route T. Global/group/route middleware runs before decoding and can reject a request without consuming its body. It wraps execution and output encoding. Limits, media checks, decoding and validation run before the business handler; the outer error pipeline renders uncommitted errors.

O defaults to JSON. Reply[O] carries status/headers/cookies; FileReply/StreamReply/RedirectReply support files, ordinary streams and redirects. FromAction accepts RequestOf[T] and defaults to 204. FromFunc/FromProcedure adapt no-input functions. Raw handles request/response access directly, retaining middleware, limits and cleanup without DTO decoding. SSE/WebSocket/upgrades are outside the current scope.

OpenAPI exports Data schemas and explicit binding parameters. WithParameter documents on-demand parameters without parsing or required-field validation. Unknown dynamic contracts are marked unavailable. Batch preflight errors install nothing; backend failures can leave partial registration without rollback.

## Migration and verification

Breaking differences from v2: HTTP constructors accept RequestOf[T] instead of arbitrary I; WithInput configures Data instead of the complete input; tagged binding requires explicit BindInput. v2 retains its existing API. Remove the outer DecodeRequest from `WithInput(DecodeRequest(codec))` when migrating.

Checks: `go test ./ghttp/v3`, `go test -race ./ghttp/v3`, `make check PKGS=./ghttp/v3`. No equivalent Gin/Echo benchmark has established performance parity.

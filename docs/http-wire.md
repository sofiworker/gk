# HTTP wire format

**English** | [简体中文](zh-CN/http-wire.md) | [Documentation](README.md)

[Source](../http/wire/) · `github.com/sofiworker/keel/http/wire`

Shared protocol primitives used by the server, client and application code.
The package imports only the standard library and contains no retry, routing or response policy.

```text
http server ──→ http/wire ←── http/client
```

## Capabilities

| Area | API | Behavior |
| --- | --- | --- |
| Media types | `MediaType`, `ContentTypeIn`, `ContentType*` | Normalize the portion before `;`; allow an empty Content-Type |
| Strict decoding | `DecodeJSON`, `DecodeXML`, `ErrInvalidFormat` | Accept exactly one value and reject trailing content |
| Decode options | `RequireBody()`, `DisallowUnknownFields()` | Defaults permit empty bodies and unknown fields |
| SSE | `ReadSSEEvent`, `AppendSSEField`, `StripSSELineBreaks`, `SSEEvent` | WHATWG multiline data, CRLF, comments and NUL rules |
| Logs | `LogToken` | Escape control characters; does not redact sensitive data |
| Status contract | `StatusCoder`, `StatusOf` | Shared by server HTTPError and client.Error |

## Decoding

```go
// Client response: permissive toward newly added fields.
err := wire.DecodeJSON(resp.Body, &value, resp.ContentLength)

// Server request: require a body and reject unknown fields.
err = wire.DecodeJSON(req.Body, &value, req.ContentLength,
    wire.RequireBody(), wire.DisallowUnknownFields())
```

| Input | Default | With RequireBody |
| --- | --- | --- |
| Empty body with zero/unknown length | nil, destination stays zero-valued | ErrInvalidFormat |
| Empty body despite positive declared length | ErrInvalidFormat | ErrInvalidFormat |
| Non-whitespace after the first value | ErrInvalidFormat | ErrInvalidFormat |
| Exceeds http.MaxBytesReader | Preserves *http.MaxBytesError | Same |

Malformed input preserves the underlying cause, so `errors.Is` can classify
ErrInvalidFormat and `errors.As` can find a JSON syntax error.
Body limits are distinct from format errors: server ReadJSON returns 415 for an unsupported
media type, 413 for a body limit and 400 for invalid input, retaining the cause.

## Status codes

```go
if wire.StatusOf(err) == http.StatusNotFound {
    // Handle the error according to this application's policy.
}
```

StatusOf searches the error chain. The contract does not instruct a server to forward
a downstream client's status to its own caller.

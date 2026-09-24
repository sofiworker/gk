# Complete v2 HTTP example

[中文](README.md)

Run from the repository root:

```sh
go run ./ghttp/v2/examples/http -addr 127.0.0.1:8080
```

Ctrl+C initiates graceful shutdown with a five-second deadline. This development example has no database: user operations are illustrative and uploads are consumed and discarded. `Authorization: Bearer demo` demonstrates group middleware only.

| Route | Demonstrates |
| --- | --- |
| GET /health | Public health endpoint |
| GET / | Redirect |
| GET /api/users/{id} | DTO path binding and JSON/XML negotiation |
| PATCH /api/users/{id} | Path plus JSON body, pointer input/output, validation |
| POST /api/users | Strict JSON, Reply, status 201 |
| DELETE /api/users/{id} | Action, status 204 |
| GET /api/inspect | RequestInput query/header/cookie access |
| GET /api/decoded/{id} | DecodeWith |
| POST /api/form | URL-encoded form with repeated values |
| POST /api/upload | Multipart DTO |
| POST /api/upload-stream | Part-by-part upload |
| GET /api/download | Attachment, Range, HEAD, Last-Modified |
| GET /api/stream | Regular HTTP streaming, not SSE |
| GET /openapi.json | Registration metadata exported once |

All /api routes require the demo token and retain the two-MiB body limit. Example calls:

```sh
curl -i -H 'Authorization: Bearer demo' http://127.0.0.1:8080/api/users/7
curl -i -H 'Authorization: Bearer demo' -H 'Accept: application/xml' http://127.0.0.1:8080/api/users/7
curl -i -X PATCH -H 'Authorization: Bearer demo' -H 'Content-Type: application/json' -d '{"name":"alice"}' http://127.0.0.1:8080/api/users/7
curl -i -H 'Authorization: Bearer demo' -F 'file=@go.mod' http://127.0.0.1:8080/api/upload-stream
curl -i -H 'Authorization: Bearer demo' -H 'Range: bytes=0-4' http://127.0.0.1:8080/api/download
```

OpenAPI explicitly marks schemas it cannot infer and excludes its own endpoint, which is registered afterwards. The file example uses an in-memory reader; for os.File, set both Content and Closer on FileReply rather than closing it in the handler before encoding starts.

```sh
go test ./ghttp/v2/examples/http
go test -race ./ghttp/v2/examples/http
```

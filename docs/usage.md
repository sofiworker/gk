# Usage

**English** | [简体中文](zh-CN/usage.md) | [Documentation](README.md)

These complete examples use the root module and Go 1.25.12+. Save each as a separate
`main.go` in an application that requires Keel, then run `go run .`.

## HTTP handler

The server implements `net/http.Handler`. This example uses an in-process recorder,
so it does not open a listening socket:

```go
package main

import (
    "context"
    "fmt"
    stdhttp "net/http"
    "net/http/httptest"

    keelhttp "github.com/sofiworker/keel/http"
)

func main() {
    server := keelhttp.NewServer()
    route := keelhttp.Raw(stdhttp.MethodGet, "/hello",
        func(_ context.Context, _ *keelhttp.Request, response *keelhttp.Response) error {
            _, err := response.Write([]byte("hello"))
            return err
        })
    if err := server.Register(route); err != nil {
        panic(err)
    }
    recorder := httptest.NewRecorder()
    server.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, "/hello", nil))
    fmt.Println(recorder.Code, recorder.Body.String())
}
```

Output: `200 hello`. Register routes and middleware before serving the first request.
For a network service, pass the handler to a standard library HTTP server and manage its
shutdown in the application. See [Server](http.md), [client](http-client.md) and
[telemetry middleware](http-telemetry.md).

## In-memory cache

```go
package main

import (
    "context"
    "fmt"
    "time"

    cache "github.com/sofiworker/keel/cache"
)

func main() {
    store, err := cache.NewMemoryCache(cache.WithCleanupInterval(0))
    if err != nil {
        panic(err)
    }
    defer store.Close()

    ctx := context.Background()
    if err := store.Set(ctx, "greeting", []byte("hello"), time.Minute); err != nil {
        panic(err)
    }
    value, err := store.Get(ctx, "greeting")
    if err != nil {
        panic(err)
    }
    fmt.Println(string(value))
}
```

Output: `hello`. The explicit alias `cache` works with the current
`package gcache` declaration. See [Cache](cache.md) for backend contracts and local caches.

## Composition and ownership

Inject clients, loggers and telemetry through the interfaces accepted by each package.
The application owns database connections, HTTP listeners and borrowed SDK objects.
Close resources it creates. Shut down managed telemetry with a fresh context and a deadline.

Package guides also contain fragments intended to run inside an application.
Executable Agent examples are listed in [Agent examples](agent-examples.md).
Folder names and package identifiers are listed in [Migration](migration.md).

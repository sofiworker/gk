# gcache

Generic caching library supporting Memory, Redis, and Valkey.

## Usage

```go
import "github.com/sofiworker/gk/gcache"

cache := gcache.NewMemoryCache(time.Minute)
cache.Set(ctx, "key", []byte("value"), 0)
```

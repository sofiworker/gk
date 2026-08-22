# gcache

English | [中文](README.md)

`gcache` does exactly three things: it **defines cache contracts as small interfaces**, **provides functions built on those contracts**, and **ships a single in-process memory implementation**.

It **imports no client library and recognises no concrete backend**. Redis, Valkey, Memcached or any home-grown store is injected by you after implementing the interfaces below; the package neither knows nor cares what you pass in. The dependency direction is therefore always "your code → gcache", and a memory-only program links in no TLS/X.509/DNS stack.

## Why It Is Built This Way

Earlier versions bundled Redis and Valkey clients. Measurements showed that a program using only `MemoryCache` grew to **2.07x** the size of a hand-written equivalent; after the removal it drops back to **1.16x**, saving **2.04 MiB (43.7% of the original binary)**.

The bloat did not come from the clients' own symbols (about 14 KB combined) but from their transitive closure: `crypto/tls`, `crypto/x509`, `crypto/ecdsa`, `crypto/rsa`, `encoding/pem`, `net` and `net/url`. In other words, a user who wanted an in-memory map was forced to link a complete TLS and DNS stack.

Deciding which dependencies to pull in belongs to the user, not to a cache library. `scripts/check-deps.sh` asserts in CI that `gcache` has exactly **0** third-party dependencies.

## Features

- **Zero third-party dependencies**: standard library only, enforced by a CI assertion.
- **Composed small interfaces**: the single-method `Getter`/`Setter`/`Deleter` compose into `KV`; `Exister`, `TTLReader`, `Expirer`, `Counter` and `Pinger` stand alone and are implemented only when supported.
- **Capability detection at compile or wiring time**: a capability is expressed by interface satisfaction rather than by a call that may return "unsupported".
- **Injected backends**: implement three methods to plug in any backend, or pass closures through `Funcs`.
- **Function layer**: `GetOrLoad`, `Exists`, `Incr`/`Decr` and `GetJSON[T]`/`SetJSON[T]` work with any injected implementation.
- **Conformance suites**: `gcache/cachetest` lets you prove your injected implementation honours the contracts.
- **Local caches**: the generic `LRUCache[K, V]`, `LFUCache[K, V]` and `TimedCache[K, V]` cover three in-process eviction policies, each with built-in locking and thread-safe by default.

## Installation

```bash
go get github.com/sofiworker/gk/gcache
```

## Contracts

The only contract a backend must satisfy is `KV`, i.e. three methods:

```go
type Getter interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

type Setter interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type Deleter interface {
	Delete(ctx context.Context, key string) error
}

// KV 是后端唯一必需的契约。
// KV is the only contract a backend must satisfy.
type KV interface {
	Getter
	Setter
	Deleter
}
```

Every other capability is optional and stands on its own:

```go
type Exister interface {
	Exists(ctx context.Context, key string) (bool, error)
}

type TTLReader interface {
	TTL(ctx context.Context, key string) (time.Duration, error)
}

type Expirer interface {
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// Add 以 delta 原子增减，delta 为负即递减。
// Add atomically adds delta; a negative delta decrements.
type Counter interface {
	Add(ctx context.Context, key string, delta int64) (int64, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}
```

Closing resources uses the standard library's `io.Closer`; the package defines no equivalent of its own.

### Contract Details

| Rule | Meaning |
|------|---------|
| Miss | `Get` must return `ErrCacheMiss`; the injecting side translates the backend's own miss error |
| `ttl <= 0` | Means no expiration |
| Deleting an absent key | Idempotent, must not return an error |
| A negative `TTL` result | Means the key never expires; an absent key yields `ErrCacheMiss` |
| `Expire` | Returns `ErrCacheMiss` for an absent key; `ttl <= 0` clears the expiration instead of deleting the key |
| The slice returned by `Get` | Is owned by the caller; mutating it must not affect cached contents |

### Why There Is No hash / list / set / CONFIG

After surveying Redis, Valkey, Dragonfly, KeyDB, Garnet, ElastiCache, Upstash, Memcached and etcd, plus ristretto, bigcache, freecache, patrickmn/go-cache, hashicorp/golang-lru, otter and ttlcache:

- **hash/list/set are exclusive to the Redis family.** Memcached, etcd and every Go in-process library lack them. Putting `HashGet` into a general interface forces every non-Redis implementation to ship "unsupported" stubs — exactly where the old `MemoryCache`'s 20 stub methods came from. When you need Redis data structures, reaching for the Redis client directly is the shortest path.
- **CONFIG is a server administration command, not a cache data operation**, and managed offerings disable it (ElastiCache uses parameter groups, Upstash REST does not expose administrative commands). The same applies to `INFO` and `FLUSHDB`.
- **The real common denominator is only Get/Set/Delete plus write-time TTL.** Even reading the remaining TTL is not universal (Memcached cannot), so it is an optional capability.

## The Bundled Implementation: MemoryCache

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sofiworker/gk/gcache"
)

func main() {
	cache, err := gcache.NewMemoryCache(gcache.WithCleanupInterval(time.Minute))
	if err != nil {
		fmt.Println("Error creating MemoryCache:", err)
		return
	}
	defer cache.Close()

	ctx := context.Background()

	// ttl 为 0 表示永不过期；a ttl of 0 means the key never expires.
	if err := cache.Set(ctx, "mykey", []byte("myvalue"), 0); err != nil {
		fmt.Println("Error setting key:", err)
		return
	}

	val, err := cache.Get(ctx, "mykey")
	if err != nil {
		fmt.Println("Error getting key:", err)
		return
	}
	fmt.Printf("mykey: %s\n", val) // Output: mykey: myvalue

	if err := cache.Set(ctx, "expiring_key", []byte("this will expire"), 100*time.Millisecond); err != nil {
		fmt.Println("Error setting expiring key:", err)
		return
	}
	time.Sleep(150 * time.Millisecond)

	if _, err := cache.Get(ctx, "expiring_key"); errors.Is(err, gcache.ErrCacheMiss) {
		fmt.Println("expiring_key: cache miss as expected")
	}
}
```

`MemoryCache` implements `KV`, `Exister`, `TTLReader`, `Expirer`, `Counter`, `Pinger` and `io.Closer`. Passing a non-positive value to `WithCleanupInterval` disables the background cleanup goroutine; expired items are still evicted on read.

## Plugging In Your Own Backend

### Form One: Implement the Interfaces (Main Path)

Only three methods are required, so wrapping any backend takes a few dozen lines. **The snippet below is your code, not part of this package** — `gcache` does not depend on redis and does not provide this type:

```go
package mycache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sofiworker/gk/gcache"
)

type RedisStore struct{ c *redis.Client }

func NewRedisStore(c *redis.Client) *RedisStore { return &RedisStore{c: c} }

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := s.c.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		// 错误翻译由注入方负责，只有这一层知道后端的错误约定；
		// the injecting side owns error translation, as only it knows the backend's conventions.
		return nil, gcache.ErrCacheMiss
	}
	return b, err
}

func (s *RedisStore) Set(ctx context.Context, key string, v []byte, ttl time.Duration) error {
	return s.c.Set(ctx, key, v, ttl).Err()
}

func (s *RedisStore) Delete(ctx context.Context, key string) error {
	return s.c.Del(ctx, key).Err()
}
```

To offer an extra capability, implement one more interface; nothing in `gcache` has to change:

```go
func (s *RedisStore) Add(ctx context.Context, key string, delta int64) (int64, error) {
	return s.c.IncrBy(ctx, key, delta).Result()
}
```

The essential point: you pass in an **already-constructed client** rather than connection options invented by this package. TLS, Cluster, Sentinel, credential providers and hooks all stay under your control instead of being narrowed by a wrapper that can never keep up with the upstream client.

### Form Two: Closure Injection

One-off wrappers and test doubles can use `Funcs` directly, without declaring a type for three methods:

```go
store := gcache.Funcs{
	GetFunc:    func(ctx context.Context, key string) ([]byte, error) { /* ... */ },
	SetFunc:    func(ctx context.Context, key string, v []byte, ttl time.Duration) error { /* ... */ },
	DeleteFunc: func(ctx context.Context, key string) error { /* ... */ },
}
```

A nil field makes the corresponding method return `ErrNotImplemented`. `Funcs` deliberately covers `KV` only: adding fields such as `ExistsFunc` or `TTLFunc` would make it satisfy those interfaces unconditionally and break the type-assertion capability detection below. Declare your own type when you need optional capabilities.

## The Function Layer

These functions hold for any injected implementation and are the package's main value now that backends are gone.

### GetOrLoad

Loads and stores on a miss, replacing the old `GetOrSet` that was duplicated across three implementations:

```go
v, err := gcache.GetOrLoad(ctx, store, "user:42", time.Minute,
	func(ctx context.Context) ([]byte, error) {
		return fetchFromDB(ctx, 42)
	})
```

A read error other than `ErrCacheMiss` propagates, so a broken backend is never silently degraded into unnoticed origin traffic. If the write-back fails, the loaded value is returned alongside the error so the caller can still decide to use it.

### Exists: Graceful Degradation

```go
ok, err := gcache.Exists(ctx, store, "user:42")
```

Backends implementing `Exister` use the native check; others fall back to `Get`. A backend without EXISTS therefore never has to ship a stub just to satisfy an interface.

### Counters and Typed Access

```go
n, err := gcache.Incr(ctx, counter, "views", 1)
n, err = gcache.Decr(ctx, counter, "quota", 5)

type User struct {
	Name string `json:"name"`
}
err = gcache.SetJSON(ctx, store, "user:42", User{Name: "gk"}, time.Minute)
u, err := gcache.GetJSON[User](ctx, store, "user:42")
```

## Capability Detection

Capabilities are expressed by interface satisfaction, so they are settled at compile time or once at the wiring site instead of on every call:

```go
// 编译期约束：调用点只声明自己真正需要的能力；
// compile-time constraint: the call site declares only what it actually needs.
func warm(ctx context.Context, c interface {
	gcache.KV
	gcache.Counter
}) error {
	// ...
}

// 装配期一次性探测可选能力；
// detect an optional capability once, at wiring time.
if r, ok := store.(gcache.TTLReader); ok {
	ttl, err := r.TTL(ctx, "user:42")
	// ...
}
```

## Conformance Testing

`gcache/cachetest` provides conformance suites split by capability, so an implementation runs exactly the suites it supports. It depends only on the standard library's `testing`:

```go
package mycache_test

import (
	"testing"

	"github.com/sofiworker/gk/gcache/cachetest"
)

func TestRedisStoreConformance(t *testing.T) {
	store := newRedisStoreForTest(t)

	t.Run("KV", func(t *testing.T) { cachetest.RunKV(t, store) })
	t.Run("TTL", func(t *testing.T) { cachetest.RunTTL(t, store) })
	t.Run("Counter", func(t *testing.T) { cachetest.RunCounter(t, store) })
}
```

`RunTTL` automatically skips the `TTLReader`/`Expirer` assertions an implementation does not provide, so a `KV`-only backend can run it too.

## Local Caches

`LRUCache[K, V]`, `LFUCache[K, V]` and `TimedCache[K, V]` are in-process caches independent of the contracts above: they store Go values directly through generics and skip serialization, whereas the byte-oriented `KV` exists for backends that put data on the wire. The two are kept deliberately separate.

All three have **built-in locking and are thread-safe by default**; no wrapper is needed. `LRUCache`/`LFUCache` reorder entries on `Get` (mutating internal state), so every operation takes the same `sync.Mutex`; `TimedCache`'s `Get` is read-only and uses a `sync.RWMutex`.

### LRUCache

```go
lru := gcache.NewLRUCache[string, any](2)
lru.Set("key1", "value1")
lru.Set("key2", 123)

val, ok := lru.Get("key1") // 访问 key1，使其成为最近使用的；mark key1 as recently used.
lru.Set("key3", true)      // 容量已满，key2 被淘汰；full, so key2 is evicted.
```

### LFUCache

```go
lfu := gcache.NewLFUCache[string, string](2)
lfu.Set("key1", "value1")
lfu.Set("key2", "value2")

lfu.Get("key1")           // key1 频率升到 2；key1's frequency becomes 2.
lfu.Set("key3", "value3") // 容量已满，频率最低的 key2 被淘汰；full, so the least frequent key2 is evicted.
```

### TimedCache

```go
cache := gcache.NewTimedCache[string, string](100 * time.Millisecond) // 每 100ms 清理一次；reap every 100ms.
defer cache.Close()

cache.Set("short_lived", "gone soon", 50*time.Millisecond)
cache.Set("never_expires", "forever", 0) // ttl = 0 表示永不过期；a ttl of 0 never expires.

val, ok := cache.Get("short_lived")
```

## Migrating From Older Versions

This repository is pre-v1.0.0; every item below is a breaking change.

| Change | Migration |
|--------|-----------|
| `NewRedisCache` / `NewValkeyCache` and `RedisCache` / `ValkeyCache` removed | Inject your own via form one above (~40 lines), or use `Funcs` closures |
| `Options` lost 9 connection fields and their `With*` setters | Configure `redis.Options` / `valkey.ClientOption` directly, which is strictly more capable |
| All `*WithContext` twin methods removed | Use the single ctx-first method set, e.g. `Set(ctx, key, value, ttl)` |
| `Increment` / `Decrement` → `Add` | `Add(ctx, key, n)` / `Add(ctx, key, -n)`, or the `Incr` / `Decr` helpers |
| `Hash*` / `List*` / `Set*` interfaces and methods removed | Use the Redis client directly when you need Redis data structures |
| The `GetOrSet` method → the `GetOrLoad` function | `gcache.GetOrLoad(ctx, cache, key, ttl, load)` |
| `Cache` / `CacheWithContext` / `BasicCache` composites removed | Compose the small interfaces at the call site |
| `Serializer` / `JSONSerializer` removed | Use `GetJSON[T]` / `SetJSON[T]`; for other encodings serialize yourself and call `Set` |
| `ErrNotSupported` removed | Capabilities are determined by type assertion; `Funcs` returns `ErrNotImplemented` for missing fields |
| `Expire` semantics | Returns `ErrCacheMiss` for absent keys; `ttl <= 0` now clears the expiration instead of silently doing nothing |
| `LRUCache` / `LFUCache` / `TimedCache` are now generic | Supply type parameters at construction, e.g. `NewLRUCache[string, any](2)`; values are no longer `interface{}`, so reads need no type assertion |
| `ThreadSafeLRUCache` / `ThreadSafeLFUCache` and their constructors removed | The three local caches now have built-in locking and are thread-safe by default; use `NewLRUCache` / `NewLFUCache` directly |

## Running Tests

```bash
go test ./gcache/...
```

No test depends on an external service any more: the package contains no remote backend, so the whole suite runs offline.

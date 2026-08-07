# gcache

`gcache` 是一个通用的 Go 缓存库，支持多种缓存实现，包括内存缓存、基于 Redis 和 Valkey 的分布式缓存，以及 LRU、LFU 和基于超时的本地缓存。它旨在提供灵活且易于使用的缓存解决方案。
`gcache` is a general-purpose Go caching library supporting in-memory, Redis- and Valkey-backed caches, plus local LRU/LFU and TTL-based caches, designed to be flexible and easy to use.

## 特性 / Features

*   **多种缓存后端 / Multiple backends**：
    *   `MemoryCache`: 简单的内存缓存，支持过期时间。
        Simple in-memory cache with expiration.
    *   `RedisCache`: 基于 Redis 的分布式缓存。
        Redis-backed distributed cache.
    *   `ValkeyCache`: 基于 Valkey 的分布式缓存。
        Valkey-backed distributed cache.
    *   `LRUCache`: 本地 LRU (Least Recently Used) 缓存，支持线程安全和非线程安全版本。
        Local LRU cache with thread-safe and non-thread-safe variants.
    *   `LFUCache`: 本地 LFU (Least Frequently Used) 缓存，支持线程安全和非线程安全版本。
        Local LFU cache with thread-safe and non-thread-safe variants.
    *   `TimedCache`: 本地基于超时淘汰的缓存，支持后台自动清理。
        Local TTL cache with background cleanup.
*   **统一接口 / Unified interface**：为分布式缓存提供了 `Cache` 和 `CacheWithContext` 接口，方便切换不同的实现。
    `Cache` and `CacheWithContext` interfaces make implementations swappable.
*   **GetOrSet**：`GetOrSet`/`GetOrSetWithContext` 以 loader 模式在未命中时自动加载并写入。
    `GetOrSet`/`GetOrSetWithContext` load-and-store on cache miss via a loader.
*   **灵活配置 / Flexible configuration**：通过 `Option` 模式进行配置。
    Options are configured via the `Option` pattern.
*   **错误处理 / Error handling**：统一的 `ErrCacheMiss` 错误表示缓存未命中。
    A single `ErrCacheMiss` represents a cache miss.

## 安装 / Installation

使用 `go get` 命令安装 `gcache`：
Install `gcache` with `go get`:

```bash
go get github.com/sofiworker/gk/gcache
```

## 通用接口 / Common Interfaces

`gcache` 为 `RedisCache` 和 `ValkeyCache` 提供了以下通用接口：
`gcache` provides the following common interfaces for `RedisCache` and `ValkeyCache`:

```go
// Cache 定义缓存的基本操作。
// Cache defines the basic operations for a cache.
type Cache interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, expiration time.Duration) error
	Delete(key string) error
	Exists(key string) (bool, error)
	Expire(key string, expiration time.Duration) error
	TTL(key string) (time.Duration, error)
	Increment(key string, value int64) (int64, error)
	Decrement(key string, value int64) (int64, error)
	HashSet(key string, field string, value []byte) error
	HashGet(key string, field string) ([]byte, error)
	HashGetAll(key string) (map[string][]byte, error)
	HashDelete(key string, fields ...string) error
	ListPush(key string, values ...[]byte) error
	ListPop(key string) ([]byte, error)
	ListRange(key string, start, stop int64) ([][]byte, error)
	SetAdd(key string, members ...[]byte) error
	SetMembers(key string) ([][]byte, error)
	SetIsMember(key string, member []byte) (bool, error)
	Close() error
	Ping() error
}

// CacheWithContext 定义接受 context 的缓存操作。
// CacheWithContext defines cache operations that accept a context.
type CacheWithContext interface {
	GetWithContext(ctx context.Context, key string) ([]byte, error)
	SetWithContext(ctx context.Context, key string, value []byte, expiration time.Duration) error
	DeleteWithContext(ctx context.Context, key string) error
	ExistsWithContext(ctx context.Context, key string) (bool, error)
	ExpireWithContext(ctx context.Context, key string, expiration time.Duration) error
	TTLWithContext(ctx context.Context, key string) (time.Duration, error)
	IncrementWithContext(ctx context.Context, key string, value int64) (int64, error)
	DecrementWithContext(ctx context.Context, key string, value int64) (int64, error)
	HashSetWithContext(ctx context.Context, key string, field string, value []byte) error
	HashGetWithContext(ctx context.Context, key string, field string) ([]byte, error)
	HashGetAllWithContext(ctx context.Context, key string) (map[string][]byte, error)
	HashDeleteWithContext(ctx context.Context, key string, fields ...string) error
	ListPushWithContext(ctx context.Context, key string, values ...[]byte) error
	ListPopWithContext(ctx context.Context, key string) ([]byte, error)
	ListRangeWithContext(ctx context.Context, key string, start, stop int64) ([][]byte, error)
	SetAddWithContext(ctx context.Context, key string, members ...[]byte) error
	SetMembersWithContext(ctx context.Context, key string) ([][]byte, error)
	SetIsMemberWithContext(ctx context.Context, key string, member []byte) (bool, error)
	PingWithContext(ctx context.Context) error
}
```

## 使用示例 / Usage Examples

### MemoryCache

`MemoryCache` 是一个简单的进程内缓存，支持基于时间的过期淘汰。

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sofiworker/gk/gcache"
)

func main() {
	// 创建一个 MemoryCache，并设置每分钟清理一次过期项；
	// create a MemoryCache with a one-minute cleanup interval.
	cache, err := gcache.NewMemoryCache(gcache.WithCleanupInterval(time.Minute))
	if err != nil {
		fmt.Println("Error creating MemoryCache:", err)
		return
	}
	defer cache.Close() // 确保在应用退出时关闭清理 goroutine；ensure cleanup stops on exit.

	ctx := context.Background()

	// 设置一个永不过期的键值对；
	// set a key that never expires.
	err = cache.Set(ctx, "mykey", []byte("myvalue"), 0)
	if err != nil {
		fmt.Println("Error setting key:", err)
		return
	}

	// 获取值；
	// get the value.
	val, err := cache.Get(ctx, "mykey")
	if err != nil {
		fmt.Println("Error getting key:", err)
		return
	}
	fmt.Printf("MemoryCache - mykey: %s\n", string(val)) // Output: MemoryCache - mykey: myvalue

	// 设置一个带过期时间的键
	err = cache.Set(ctx, "expiring_key", []byte("this will expire"), 100*time.Millisecond)
	if err != nil {
		fmt.Println("Error setting expiring key:", err)
		return
	}

	time.Sleep(150 * time.Millisecond) // 等待过期；wait for expiration.

	_, err = cache.Get(ctx, "expiring_key")
	if err == gcache.ErrCacheMiss {
		fmt.Println("MemoryCache - expiring_key: Cache miss as expected") // Output: MemoryCache - expiring_key: Cache miss as expected
	} else if err != nil {
		fmt.Println("Error getting expiring key:", err)
	}
}
```

### RedisCache

`RedisCache` 使用 Redis 作为后端存储，提供分布式缓存能力。

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sofiworker/gk/gcache"
)

func main() {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379" // 默认 Redis 地址；default Redis address.
	}

	// 创建一个 RedisCache 客户端；
	// create a RedisCache client.
	cache, err := gcache.NewRedisCache(gcache.WithAddress(redisAddr))
	if err != nil {
		fmt.Println("Error creating RedisCache:", err)
		return
	}
	defer cache.Close() // 确保关闭 Redis 连接；ensure the Redis connection closes.

	ctx := context.Background()

	// 设置一个带 10 秒过期时间的键值对；
	// set a key with a 10-second TTL.
	err = cache.Set(ctx, "redis_key", []byte("hello from redis"), 10*time.Second)
	if err != nil {
		fmt.Println("Error setting Redis key:", err)
		return
	}

	// 获取值；
	// get the value.
	val, err := cache.Get(ctx, "redis_key")
	if err != nil {
		fmt.Println("Error getting Redis key:", err)
		return
	}
	fmt.Printf("RedisCache - redis_key: %s\n", string(val)) // Output: RedisCache - redis_key: hello from redis

	// 删除一个键；
	// delete a key.
	err = cache.Delete(ctx, "redis_key")
	if err != nil {
		fmt.Println("Error deleting Redis key:", err)
		return
	}

	_, err = cache.Get(ctx, "redis_key")
	if err == gcache.ErrCacheMiss {
		fmt.Println("RedisCache - redis_key: Cache miss after deletion as expected") // Output: RedisCache - redis_key: Cache miss after deletion as expected
	} else if err != nil {
		fmt.Println("Error getting deleted Redis key:", err)
	}
}
```

### ValkeyCache

`ValkeyCache` 使用 Valkey 作为后端存储，提供分布式缓存能力。其 API 与 `RedisCache` 类似。

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sofiworker/gk/gcache"
)

func main() {
	valkeyAddr := os.Getenv("VALKEY_ADDR")
	if valkeyAddr == "" {
		valkeyAddr = "localhost:6379" // 默认 Valkey 地址；default Valkey address.
	}

	// 创建一个 ValkeyCache 客户端；
	// create a ValkeyCache client.
	cache, err := gcache.NewValkeyCache(gcache.WithAddress(valkeyAddr))
	if err != nil {
		fmt.Println("Error creating ValkeyCache:", err)
		return
	}
	defer cache.Close() // 确保关闭 Valkey 连接；ensure the Valkey connection closes.

	ctx := context.Background()

	// 设置一个带 10 秒过期时间的键值对；
	// set a key with a 10-second TTL.
	err = cache.Set(ctx, "valkey_key", []byte("hello from valkey"), 10*time.Second)
	if err != nil {
		fmt.Println("Error setting Valkey key:", err)
		return
	}

	// 获取值；
	// get the value.
	val, err := cache.Get(ctx, "valkey_key")
	if err != nil {
		fmt.Println("Error getting Valkey key:", err)
		return
	}
	fmt.Printf("ValkeyCache - valkey_key: %s\n", string(val)) // Output: ValkeyCache - valkey_key: hello from valkey

	// 递增一个计数器；
	// increment a counter.
	newVal, err := cache.Increment(ctx, "valkey_counter", 1)
	if err != nil {
		fmt.Println("Error incrementing Valkey counter:", err)
		return
	}
	fmt.Printf("ValkeyCache - valkey_counter: %d\n", newVal) // Output: ValkeyCache - valkey_counter: 1
}
```

### LRUCache (Least Recently Used)

`LRUCache` 是一种本地缓存，当容量达到上限时，会淘汰最近最少使用的项。提供线程安全和非线程安全版本。

```go
package main

import (
	"fmt"

	"github.com/sofiworker/gk/gcache"
)

func main() {
	fmt.Println("--- Non-thread-safe LRUCache ---")
	// 创建一个容量为 2 的非线程安全 LRU 缓存；
	// create a non-thread-safe LRU cache with capacity 2.
	lruCache := gcache.NewLRUCache(2)

	lruCache.Set("key1", "value1")
	lruCache.Set("key2", 123)

	fmt.Printf("LRUCache - Len: %d\n", lruCache.Len()) // Output: LRUCache - Len: 2

	val, ok := lruCache.Get("key1") // 访问 key1，使其成为最近使用；mark key1 as recently used.
	if ok {
		fmt.Printf("LRUCache - Get key1: %v\n", val) // Output: LRUCache - Get key1: value1
	}

	lruCache.Set("key3", true) // 容量已满，key2 将被淘汰；full, so key2 is evicted.

	_, ok = lruCache.Get("key2")
	if !ok {
		fmt.Println("LRUCache - key2 evicted as expected") // Output: LRUCache - key2 evicted as expected
	}

	val, ok = lruCache.Get("key1")
	if ok {
		fmt.Printf("LRUCache - Get key1: %v\n", val) // Output: LRUCache - Get key1: value1
	}
	val, ok = lruCache.Get("key3")
	if ok {
		fmt.Printf("LRUCache - Get key3: %v\n", val) // Output: LRUCache - Get key3: true
	}

	fmt.Println("\n--- Thread-safe LRUCache ---")
	// 创建一个容量为 2 的线程安全 LRU 缓存；
	// create a thread-safe LRU cache with capacity 2.
	tsLruCache := gcache.NewThreadSafeLRUCache(2)
	tsLruCache.Set("ts_key1", "ts_value1")
	val, ok = tsLruCache.Get("ts_key1")
	if ok {
		fmt.Printf("ThreadSafeLRUCache - Get ts_key1: %v\n", val) // Output: ThreadSafeLRUCache - Get ts_key1: ts_value1
	}
}
```

### LFUCache (Least Frequently Used)

`LFUCache` 是一种本地缓存，当容量达到上限时，会淘汰访问频率最低的项。提供线程安全和非线程安全版本。

```go
package main

import (
	"fmt"

	"github.com/sofiworker/gk/gcache"
)

func main() {
	fmt.Println("--- Non-thread-safe LFUCache ---")
	// 创建一个容量为 2 的非线程安全 LFU 缓存；
	// create a non-thread-safe LFU cache with capacity 2.
	lfuCache := gcache.NewLFUCache(2)

	lfuCache.Set("key1", "value1") // 频率 1；freq 1.
	lfuCache.Set("key2", "value2") // 频率 1；freq 1.

	lfuCache.Get("key1") // key1 频率变为 2；key1 freq becomes 2.
	lfuCache.Get("key1") // key1 频率变为 3；key1 freq becomes 3.

	fmt.Printf("LFUCache - Len: %d\n", lfuCache.Len()) // Output: LFUCache - Len: 2

	lfuCache.Set("key3", "value3") // 容量已满，key2 最不常用，将被淘汰；full, key2 is evicted.

	_, ok := lfuCache.Get("key2")
	if !ok {
		fmt.Println("LFUCache - key2 evicted as expected") // Output: LFUCache - key2 evicted as expected
	}

	val, ok := lfuCache.Get("key1")
	if ok {
		fmt.Printf("LFUCache - Get key1: %v\n", val) // Output: LFUCache - Get key1: value1
	}
	val, ok = lfuCache.Get("key3")
	if ok {
		fmt.Printf("LFUCache - Get key3: %v\n", val) // Output: LFUCache - Get key3: value3
	}

	fmt.Println("\n--- Thread-safe LFUCache ---")
	// 创建一个容量为 2 的线程安全 LFU 缓存；
	// create a thread-safe LFU cache with capacity 2.
	tsLfuCache := gcache.NewThreadSafeLFUCache(2)
	tsLfuCache.Set("ts_key1", "ts_value1")
	val, ok = tsLfuCache.Get("ts_key1")
	if ok {
		fmt.Printf("ThreadSafeLFUCache - Get ts_key1: %v\n", val) // Output: ThreadSafeLFUCache - Get ts_key1: ts_value1
	}
}
```

### TimedCache (Timeout Eviction)

`TimedCache` 是一种本地缓存，会根据每个项的过期时间自动淘汰。

```go
package main

import (
	"fmt"
	"time"

	"github.com/sofiworker/gk/gcache"
)

func main() {
	// 创建一个 TimedCache，并设置每 100 毫秒清理一次过期项；
	// create a TimedCache with a 100ms cleanup interval.
	cache := gcache.NewTimedCache(100 * time.Millisecond)
	defer cache.Close() // 确保在应用退出时关闭后台清理；ensure cleanup stops on exit.

	// 设置一个 50 毫秒后过期的项；
	// set an item expiring after 50ms.
	cache.Set("short_lived", "I will be gone soon", 50*time.Millisecond)
	// 设置一个 500 毫秒后过期的项；
	// set an item expiring after 500ms.
	cache.Set("long_lived", "I stay a bit longer", 500*time.Millisecond)
	// 设置一个永不过期的项 (ttl = 0)；
	// set an item that never expires (ttl = 0).
	cache.Set("never_expires", "Forever young", 0)

	fmt.Printf("TimedCache - Initial Len: %d\n", cache.Len()) // Output: TimedCache - Initial Len: 3

	time.Sleep(70 * time.Millisecond) // 等待 short_lived 过期；wait for short_lived to expire.

	val, ok := cache.Get("short_lived")
	if !ok {
		fmt.Println("TimedCache - short_lived: Not found (expired)") // Output: TimedCache - short_lived: Not found (expired)
	} else {
		fmt.Printf("TimedCache - short_lived: %v (unexpectedly found)\n", val)
	}

	val, ok = cache.Get("long_lived")
	if ok {
		fmt.Printf("TimedCache - long_lived: %v (still present)\n", val) // Output: TimedCache - long_lived: I stay a bit longer (still present)
	}

	// 等待后台清理 goroutine 运行并移除过期项；
	// wait for the background cleanup to remove expired items.
	time.Sleep(150 * time.Millisecond)
	fmt.Printf("TimedCache - Len after cleanup: %d\n", cache.Len()) // Output: TimedCache - Len after cleanup: 2 (short_lived removed)

	time.Sleep(400 * time.Millisecond) // 等待 long_lived 过期；wait for long_lived to expire.

	_, ok = cache.Get("long_lived")
	if !ok {
		fmt.Println("TimedCache - long_lived: Not found (expired)") // Output: TimedCache - long_lived: Not found (expired)
	}

	val, ok = cache.Get("never_expires")
	if ok {
		fmt.Printf("TimedCache - never_expires: %v (still present)\n", val) // Output: TimedCache - never_expires: Forever young (still present)
	}
}
```

## 运行测试 / Running Tests

要运行 `gcache` 包的所有单元测试，请在项目根目录执行以下命令：
To run all `gcache` unit tests, execute the following from the repository root:

```bash
go test ./...
```

如果您只想运行特定缓存的测试，例如 `MemoryCache`：
To run only one cache's tests, e.g. `MemoryCache`:

```bash
go test -run TestMemoryCache ./gcache
```

对于需要外部服务的测试（如 `RedisCache` 和 `ValkeyCache`），请确保相应的服务正在运行，并且可以通过 `REDIS_ADDR` 或 `VALKEY_ADDR` 环境变量进行配置。例如：
For tests needing external services (`RedisCache`/`ValkeyCache`), ensure the services are running and configurable via `REDIS_ADDR` or `VALKEY_ADDR`. For example:

```bash
REDIS_ADDR="localhost:6379" go test -run TestRedisCache ./gcache
VALKEY_ADDR="localhost:6379" go test -run TestValkeyCache ./gcache
```

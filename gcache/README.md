# gcache

[English](README.en.md) | 中文

`gcache` 只做三件事：**定义缓存契约（小接口）**、**提供基于这些契约的函数**、**自带唯一一个进程内内存实现**。

它**不 import 任何客户端库，也不认识具体后端**。Redis、Valkey、Memcached 或任何自研存储，都由你实现下面的接口后传入；本包不关心传入的是什么。因此依赖方向永远是「你的代码 → gcache」，只用内存缓存的程序不会链入 TLS/X.509/DNS 栈。

## 为什么这样设计

早期版本内置了 Redis 与 Valkey 客户端。实测表明，一个只使用 `MemoryCache` 的程序会因此膨胀到手写等价实现的 **2.07 倍**；移除后回落到 **1.16 倍**，单个二进制省下 **2.04 MiB（原体积的 43.7%）**。

膨胀主因不是这两个客户端自身的符号（合计约 14 KB），而是它们拖入的传递闭包：`crypto/tls`、`crypto/x509`、`crypto/ecdsa`、`crypto/rsa`、`encoding/pem`、`net`、`net/url`。换句话说，只想要一个内存 map 的用户被迫链入完整的 TLS 与 DNS 栈。

由用户决定引入哪些依赖，比由缓存库替用户决定更合理。`scripts/check-deps.sh` 会在 CI 中断言 `gcache` 的第三方依赖数恒为 **0**。

## 特性

- **零第三方依赖**：仅依赖标准库，由 CI 断言锁死。
- **小接口组合**：`Getter`/`Setter`/`Deleter` 三个单方法接口组合出 `KV`；`Exister`/`TTLReader`/`Expirer`/`Counter`/`Pinger` 各自独立、按需实现。
- **能力探测在编译期或装配期**：能力用「是否实现该接口」表达，而不是每次调用都可能返回「不支持」。
- **注入式后端**：实现三个方法即可接入任意后端，或用 `Funcs` 直接传闭包。
- **函数层**：`GetOrLoad`、`Exists`、`Incr`/`Decr`、`GetJSON[T]`/`SetJSON[T]` 对任何注入实现都成立。
- **契约一致性套件**：`gcache/cachetest` 让你自证注入的实现符合契约。
- **本地缓存**：泛型 `LRUCache[K, V]`、`LFUCache[K, V]`、`TimedCache[K, V]` 三种进程内淘汰策略，均内建锁、默认线程安全。

## 安装

```bash
go get github.com/sofiworker/gk/gcache
```

## 契约

后端必须满足的只有 `KV`，即三个方法：

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
type KV interface {
	Getter
	Setter
	Deleter
}
```

其余能力全部可选，各自独立：

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
type Counter interface {
	Add(ctx context.Context, key string, delta int64) (int64, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}
```

关闭资源直接用标准库的 `io.Closer`，本包不自定义。

### 契约细则

| 约定 | 说明 |
|------|------|
| 未命中 | `Get` 必须返回 `ErrCacheMiss`；注入方负责把后端自己的未命中错误翻译过来 |
| `ttl <= 0` | 表示不过期 |
| 删除不存在的键 | 幂等操作，不得返回错误 |
| `TTL` 返回负值 | 表示该键永不过期；键不存在时返回 `ErrCacheMiss` |
| `Expire` | 键不存在时返回 `ErrCacheMiss`；`ttl <= 0` 表示清除过期时间而非删除键 |
| `Get` 返回的切片 | 归调用方所有，修改它不得影响缓存内容 |

### 为什么没有 hash / list / set / CONFIG

调查过 Redis、Valkey、Dragonfly、KeyDB、Garnet、ElastiCache、Upstash、Memcached、etcd，以及 ristretto、bigcache、freecache、patrickmn/go-cache、hashicorp/golang-lru、otter、ttlcache 之后，结论是：

- **hash/list/set 是 Redis 族独有**。Memcached、etcd 和全部 Go 进程内库都不支持。把 `HashGet` 放进通用接口，等于要求所有非 Redis 实现写「不支持」的桩 —— 旧版 `MemoryCache` 的 20 个桩方法正是这么来的。需要 Redis 数据结构时，直接用 Redis 客户端最直接。
- **CONFIG 是服务器管理命令，不是缓存数据操作**，且在托管形态上被禁用（ElastiCache 改用参数组，Upstash REST 不开放管理类命令）。`INFO`、`FLUSHDB` 同理。
- **真正的公共分母只有 Get/Set/Delete 加写入时 TTL**。连读取剩余 TTL 都不通用（Memcached 读不了），所以它是可选能力。

## 自带实现：MemoryCache

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

	// ttl 为 0 表示永不过期
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

`MemoryCache` 实现了 `KV`、`Exister`、`TTLReader`、`Expirer`、`Counter`、`Pinger` 与 `io.Closer`。`WithCleanupInterval` 传入非正值即关闭后台清理 goroutine，此时过期项仍会在读取时被剔除。

## 接入自己的后端

### 形式一：实现接口（主路径）

必需方法只有三个，包装任何后端都是几十行的事。**下面是你的代码，不是本包的一部分** —— `gcache` 不依赖 redis，也不提供这个类型：

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
		return nil, gcache.ErrCacheMiss // 错误翻译由注入方负责，只有这一层知道后端的错误约定
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

要提供额外能力，就多实现一个接口，不需要改动 `gcache`：

```go
func (s *RedisStore) Add(ctx context.Context, key string, delta int64) (int64, error) {
	return s.c.IncrBy(ctx, key, delta).Result()
}
```

关键点是：你传入的是**已经构造好的客户端**，而不是本包自研的连接选项。TLS、Cluster、Sentinel、凭据 provider、hooks 全部由你掌控，不会被一层追不上上游的配置包装削减。

### 形式二：闭包注入

一次性包装或测试替身可以直接用 `Funcs`，不必为三个方法专门定义类型：

```go
store := gcache.Funcs{
	GetFunc:    func(ctx context.Context, key string) ([]byte, error) { /* ... */ },
	SetFunc:    func(ctx context.Context, key string, v []byte, ttl time.Duration) error { /* ... */ },
	DeleteFunc: func(ctx context.Context, key string) error { /* ... */ },
}
```

字段为 `nil` 时对应方法返回 `ErrNotImplemented`。`Funcs` 刻意只覆盖 `KV`：如果它也带上 `ExistsFunc`、`TTLFunc` 等字段，就会无条件满足那些接口，让下面的类型断言式能力探测失效。需要可选能力时请定义自己的类型。

## 函数层

这些函数对任何注入实现都成立，是移除后端之后本包的主要价值。

### GetOrLoad

未命中时加载并写回，取代旧版分散在三个实现里的 `GetOrSet`：

```go
v, err := gcache.GetOrLoad(ctx, store, "user:42", time.Minute,
	func(ctx context.Context) ([]byte, error) {
		return fetchFromDB(ctx, 42)
	})
```

读取遇到 `ErrCacheMiss` 以外的错误会直接上抛，避免后端故障被静默降级成无人察觉的缓存穿透。写回失败时会同时返回已加载的值与错误，让调用方自行决定是否使用。

### Exists：优雅降级

```go
ok, err := gcache.Exists(ctx, store, "user:42")
```

后端实现了 `Exister` 就用原生判断，否则回退到 `Get`。这样「没有 EXISTS」的后端不必为了满足接口而写桩。

### 计数器与类型化读写

```go
n, err := gcache.Incr(ctx, counter, "views", 1)
n, err = gcache.Decr(ctx, counter, "quota", 5)

type User struct {
	Name string `json:"name"`
}
err = gcache.SetJSON(ctx, store, "user:42", User{Name: "gk"}, time.Minute)
u, err := gcache.GetJSON[User](ctx, store, "user:42")
```

## 能力探测

能力用「是否实现该接口」表达，因此在编译期或装配处一次性确定，而不是每次调用都可能失败：

```go
// 编译期约束：调用点只声明自己真正需要的能力
func warm(ctx context.Context, c interface {
	gcache.KV
	gcache.Counter
}) error {
	// ...
}

// 装配期一次性探测可选能力
if r, ok := store.(gcache.TTLReader); ok {
	ttl, err := r.TTL(ctx, "user:42")
	// ...
}
```

## 契约一致性测试

`gcache/cachetest` 提供按能力拆分的一致性套件，实现支持哪些就跑哪些。它只依赖标准库 `testing`：

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

`RunTTL` 会自动跳过实现未提供的 `TTLReader`/`Expirer` 断言，因此只实现 `KV` 的后端同样可以跑。

## 本地缓存

`LRUCache[K, V]`、`LFUCache[K, V]`、`TimedCache[K, V]` 是独立于上述契约的进程内缓存：它们以泛型直接存 Go 值，省去序列化，而字节导向的 `KV` 是为「要过线」的后端准备的。两者刻意分开。

三者都**内建锁、默认线程安全**，无需再包一层。`LRUCache`/`LFUCache` 的 `Get` 会调整访问顺序（写内部状态），因此每个操作都取同一把 `sync.Mutex`；`TimedCache` 的 `Get` 是只读的，用 `sync.RWMutex`。

### LRUCache

```go
lru := gcache.NewLRUCache[string, any](2)
lru.Set("key1", "value1")
lru.Set("key2", 123)

val, ok := lru.Get("key1") // 访问 key1，使其成为最近使用的
lru.Set("key3", true)      // 容量已满，key2 被淘汰
```

### LFUCache

```go
lfu := gcache.NewLFUCache[string, string](2)
lfu.Set("key1", "value1")
lfu.Set("key2", "value2")

lfu.Get("key1")           // key1 频率升到 2
lfu.Set("key3", "value3") // 容量已满，频率最低的 key2 被淘汰
```

### TimedCache

```go
cache := gcache.NewTimedCache[string, string](100 * time.Millisecond) // 每 100ms 清理一次
defer cache.Close()

cache.Set("short_lived", "gone soon", 50*time.Millisecond)
cache.Set("never_expires", "forever", 0) // ttl = 0 表示永不过期

val, ok := cache.Get("short_lived")
```

## 从旧版本迁移

本仓库处于 pre-v1.0.0，以下均为破坏性变更。

| 变更 | 迁移方式 |
|------|----------|
| 删除 `NewRedisCache` / `NewValkeyCache` 与 `RedisCache` / `ValkeyCache` | 按上文形式一自行注入（约 40 行），或用 `Funcs` 闭包注入 |
| `Options` 移除 9 个连接字段与对应 `With*` | 直接配置 `redis.Options` / `valkey.ClientOption`，能力更全 |
| 删除全部 `*WithContext` 孪生方法 | 统一改用 ctx 优先的单一方法集，例如 `Set(ctx, key, value, ttl)` |
| `Increment` / `Decrement` → `Add` | `Add(ctx, key, n)` / `Add(ctx, key, -n)`，或用 `Incr` / `Decr` 函数 |
| 删除 `Hash*` / `List*` / `Set*` 接口与方法 | 需要 Redis 数据结构时直接用 Redis 客户端 |
| `GetOrSet` 方法 → `GetOrLoad` 函数 | `gcache.GetOrLoad(ctx, cache, key, ttl, load)` |
| 删除 `Cache` / `CacheWithContext` / `BasicCache` 等大接口 | 在调用点按需组合小接口 |
| 删除 `Serializer` / `JSONSerializer` | 改用 `GetJSON[T]` / `SetJSON[T]`；需要其他编码时自行序列化后调 `Set` |
| 删除 `ErrNotSupported` | 能力改为类型断言判定；`Funcs` 字段缺失时返回 `ErrNotImplemented` |
| `Expire` 语义 | 键不存在时返回 `ErrCacheMiss`；`ttl <= 0` 由「静默无操作」改为「清除过期时间」 |
| `LRUCache` / `LFUCache` / `TimedCache` 泛型化 | 构造时显式给出类型参数，如 `NewLRUCache[string, any](2)`；值不再是 `interface{}`，读取免类型断言 |
| 删除 `ThreadSafeLRUCache` / `ThreadSafeLFUCache` 及其构造函数 | 三种本地缓存现已内建锁、默认线程安全，直接用 `NewLRUCache` / `NewLFUCache` 即可 |

## 运行测试

```bash
go test ./gcache/...
```

不再有依赖外部服务的测试：本包不包含任何远端后端，因此全部测试都是离线的。

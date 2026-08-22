# gcache 去 Redis/Valkey 依赖 + 小接口注入式后端设计

> 状态：2026-08-22 计划已 review 通过；**P0、P1 已实施**（第 5 节），P2 留待单独 PR。
> 实施结果与验收数据见第 9 节。
> 关联：`2026-08-07-gk-module-dependency-policy.md`（三层分层原则，第 3.2 节"能力层只定义接口，注入发生在用户代码"）。
> 决策：不提供官方适配器 —— 本包只定义接口与函数，底层实现由用户传入（详见第 7 节决策记录）。

## 1. 目标

1. `gcache` 退回**纯进程内内存缓存**，不 import `go-redis` / `valkey-go`，第三方依赖归零。
2. 本包只提供**接口定义 + 基于接口的函数 + 唯一自带的内存实现**；远端后端一律**由用户传入**，本包不认识也不关心传入的是什么，**不提供任何官方适配器**。
3. 用**小接口组合**替代当前"大而全"的 `Cache` 接口。
4. 明确哪些操作（hash / list / set / CONFIG）**不属于**通用缓存契约。

## 2. 现状证据（均为本地实测，非估计）

### 2.1 二进制膨胀确认

用一个只用内存缓存的最小程序对比（同一 probe，只换 `replace` 目标）：

| 构建 | 体积 | 相对手写实现 |
|------|------|--------------|
| 手写等价内存缓存（不用 gcache） | 2,365,911 B | 1.00x |
| **现状**：import `gcache` 只用 `MemoryCache` | 4,886,751 B | **2.07x** |
| **移除 redis/valkey 后**：同一程序 | 2,750,688 B | **1.16x** |

**节省 2,136,063 B（2.04 MiB），占原二进制 43.7%。** 只想要一个内存 map 的用户，当前要为此付出一倍体积。

膨胀主因不是 redis/valkey 自身符号（仅约 14 KB），而是它们拖入的传递闭包：

```
crypto/tls  crypto/x509  crypto/ecdsa  crypto/rsa  encoding/pem  net  net/url
```

即一个内存缓存用户被迫链入完整 TLS/X.509/DNS 栈。

### 2.2 依赖面

`go list -deps ./gcache` 的第三方包：**现状 14 个 → 移除后 0 个**（纯 stdlib）。

在仓库副本实测 `go mod tidy`，**可归因于 gcache 的**根 `go.mod` 删除项恰好 4 个：

- `github.com/redis/go-redis/v9`（direct）
- `github.com/valkey-io/valkey-go`（direct）
- `github.com/cespare/xxhash/v2`（indirect，仅 go-redis 使用）
- `github.com/dgryski/go-rendezvous`（indirect，仅 go-redis 使用）

> 附带发现（与本次无关，建议单独清理）：根 `go.mod` 另有 6 个已无人使用的陈旧条目
> （`mimetype`、`go-playground/{locales,universal-translator,validator}`、`gorilla/websocket`、`leodido/go-urn`），
> 基线 `go mod tidy` 就会删除它们。**不要**把这部分混入本次变更的 diff。

### 2.3 接口过肥

- `Cache` 21 个方法 + `CacheWithContext` 20 个方法 = 一个完整实现要写 **41 个方法**。
- `MemoryCache` 中 **20 个方法只是 `return ErrNotSupported`** 桩（hash 8 + list 6 + set 6），占其方法数近半。
- 一致性测试套件被迫按具体类型开洞：

```218:219:gcache/cache_test.go
	// Only run these tests if the cache is not MemoryCache
	if _, ok := cache.(*MemoryCache); !ok {
```

测试自己要给某个实现开后门，说明**契约承诺了实现给不出的能力** —— 这就是接口过大的定义。

### 2.4 被包装掉的客户端能力（有损映射）

`Options` 暴露 9 个连接参数，却无法覆盖 go-redis/valkey-go 的真实配置面，且映射有损：

```21:31:gcache/valkey.go
	vopt := valkey.ClientOption{
		InitAddress:      []string{options.Address},
		Password:         options.Password,
		SelectDB:         options.DB,
		BlockingPoolSize: options.PoolSize,
		Dialer: net.Dialer{
			Timeout: options.DialTimeout,
		},
		ConnWriteTimeout: options.WriteTimeout,
		DisableRetry:     options.MaxRetries <= 0,
	}
```

`MinIdleConns` 与 `ReadTimeout` 被静默丢弃；`MaxRetries=5` 退化为布尔 `DisableRetry`。TLS、Cluster、Sentinel、凭据 provider、hooks 一概无法表达。**自研连接选项永远追不上上游客户端**，这本身就是反对"核心包持有客户端"的理由。

### 2.5 这批代码在本仓库从未被测试

`redis.go` + `valkey.go` 共 584 行。其集成测试在无服务器时 `t.Skipf`，实测本机 6379 端口关闭 → **恒定跳过**。且其中有一处一直不成立的断言：`ListPush` 用 `RPUSH`、`ListPop` 用 `LPOP`，推入 a,b,c 后 `LPOP` 应返回 `"a"`，测试却期望 `"c"`：

```266:272:gcache/cache_test.go
			item, err := cache.ListPop("list1")
			if err != nil {
				t.Fatalf("ListPop failed: %v", err)
			}
			if string(item) != "c" {
				t.Errorf("ListPop: expected 'c', got '%s'", string(item))
			}
```

结论：仓库为**从未验证过**的代码支付 2 MiB 依赖成本。

### 2.6 文档与代码已经漂移

`gcache/README.md` 的示例写的是 `cache.Set(ctx, "mykey", ...)`（ctx 优先、单一方法集），而代码实际是 `Set(key, value, exp)` + `SetWithContext(...)` 孪生方法集。文档早已按"更合理的那套 API"书写 —— 双方法集连自己的文档都没说服。

## 3. 调查：常见缓存的能力矩阵

判断依据：各后端**原生**支持（非客户端侧模拟）。

| 后端 | KV | 单键 TTL | 读剩余 TTL | 原子 INCR | Hash(HGET) | List | Set | CONFIG |
|------|----|----|----|----|----|----|----|----|
| Redis | 是 | 是 | 是 | 是 | **是** | 是 | 是 | 是（自托管） |
| Valkey | 是 | 是 | 是 | 是 | **是** | 是 | 是 | 是（自托管） |
| Dragonfly / KeyDB / Garnet | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 部分/受限 |
| AWS ElastiCache / MemoryDB | 是 | 是 | 是 | 是 | 是 | 是 | 是 | **否**（改用参数组） |
| Upstash（REST） | 是 | 是 | 是 | 是 | 是 | 是 | 是 | **否**（管理类命令不开放） |
| Memcached | 是 | 是 | 否 | 是 | **否** | 否 | 否 | 否（仅 stats） |
| etcd | 是 | 是（lease） | 部分（lease TTL） | 否（靠 txn） | **否** | 否 | 否 | 否 |
| ristretto | 是 | 是 | 否 | 否 | **否** | 否 | 否 | 否 |
| bigcache | 是 | **全局 TTL** | 否 | 否 | **否** | 否 | 否 | 否 |
| freecache | 是 | 是 | 是 | 否 | **否** | 否 | 否 | 否 |
| patrickmn/go-cache | 是 | 是 | 部分 | 是（类型化） | **否** | 否 | 否 | 否 |
| hashicorp/golang-lru | 是 | 仅 expirable 变体 | 否 | 否 | **否** | 否 | 否 | 否 |
| otter / ttlcache | 是 | 是 | 部分 | 否 | **否** | 否 | 否 | 否 |

### 结论

**A. Hash/List/Set 是 Redis 族独有。** Memcached、etcd 与全部 Go 进程内库都不支持。把 `HashGet` 放进通用 `Cache`，等于要求所有非 Redis 实现写 `ErrNotSupported` 桩 —— 正是 2.3 的现状。

**B. CONFIG 不该出现在缓存接口里。** 它是服务器管理命令，不是缓存数据操作；且在托管/Serverless 形态（ElastiCache、Upstash）上被禁用。需要调 CONFIG 的人要的是"运维 Redis 的能力"，不是"缓存抽象"——应直接用 Redis 客户端。

**C. 真正的公共分母只有：** `Get` / `Set` / `Delete` + **写入时 TTL**。`Exists`、`读取剩余 TTL`、`原子计数` 已是可选能力（Memcached 无法读 TTL，多数进程内库无原子计数）。

**D. 进程内库的 TTL 也不齐。** bigcache 只有全局 TTL，golang-lru 经典版无 TTL。所以连 TTL 都不能假定为强制方法。

**这直接决定了第 4.2 节的接口切分线。**

## 4. 设计决策

### 4.1 核心边界

`gcache` 核心 = 进程内内存缓存 + 后端契约（接口）+ 与后端无关的组合工具。**零第三方依赖**，并用 CI 断言锁死（4.7）。

### 4.2 小接口切分

原则：**每个接口都必须能被一个内存 map 诚实实现**；做不到的不进核心。

单一方法集，`ctx` 优先（删除全部 `*WithContext` 孪生方法，方法数 41 → 约 8）：

```go
// 最小读写单元；minimal read/write units.
type Getter interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

type Setter interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type Deleter interface {
	Delete(ctx context.Context, key string) error
}

// KV 是后端唯一必需的契约；KV is the only contract a backend must satisfy.
type KV interface {
	Getter
	Setter
	Deleter
}
```

可选能力，各自独立、按需断言：

```go
// 后端原生支持 EXISTS 时实现；implemented when the backend has native EXISTS.
type Exister interface {
	Exists(ctx context.Context, key string) (bool, error)
}

type TTLReader interface {
	TTL(ctx context.Context, key string) (time.Duration, error)
}

type Expirer interface {
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// Add 以 delta 原子增减，delta 为负即递减；Add atomically adds delta (negative to decrement).
type Counter interface {
	Add(ctx context.Context, key string, delta int64) (int64, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}
```

变更要点：

- `Increment`/`Decrement` 合并为 `Add` —— 现有 `MemoryCache` 内部本就是 `m.add(ctx, key, -value)`，两个方法是同一实现的两个门面。`Incr`/`Decr` 降级为自由函数。
- `Close` 用 stdlib `io.Closer`，不再自定义。
- **`HashCache` / `ListCache` / `SetCache` 从核心整体删除**（依据 3.A）。
- **不引入** CONFIG / INFO / FLUSHDB（依据 3.B）。
- 不提供 `Cache`/`BasicCache` 这类大composite；调用点按需就地组合，例如 `func warm(c interface{ gcache.KV; gcache.Counter })`。仅保留 `KV` 一个具名组合。

### 4.3 能力探测：从运行期错误改为编译期

现状：`MemoryCache.HashGet` 运行时返回 `ErrNotSupported`，调用方在生产才发现。
目标：能力用"是否实现该接口"表达 —— 要么编译期发现，要么在装配处一次性类型断言，而不是每次调用都可能失败。

优雅降级放在自由函数里，而不是逼每个实现都写桩：

```go
// Exists 优先用后端原生 EXISTS，否则回退到 Get；prefers native EXISTS, falls back to Get.
func Exists(ctx context.Context, c Getter, key string) (bool, error) {
	if e, ok := c.(Exister); ok {
		return e.Exists(ctx, key)
	}
	_, err := c.Get(ctx, key)
	if errors.Is(err, ErrCacheMiss) {
		return false, nil
	}
	return err == nil, err
}
```

### 4.4 GetOrSet 下沉为自由函数

现状同一段 loader 逻辑在 `memory.go`、`redis.go`、`valkey.go` **重复三份**。改为对接口一次实现：

```go
// GetOrLoad 未命中时调用 load 并写回；on miss, calls load and stores the result.
func GetOrLoad(ctx context.Context, c KV, key string, ttl time.Duration,
	load func(context.Context) ([]byte, error)) ([]byte, error)
```

收益：任何满足 `KV` 的后端（含用户自研）自动获得该能力；未来加 singleflight 去重只改一处。

### 4.5 注入机制

**定位（2026-08-22 决策）**：`gcache` 只提供三样东西 —— **接口定义**、**基于接口的函数/组合工具**、**唯一自带的进程内内存实现**。远端后端全部由用户传入；**本包不认识也不关心传入的是什么**，不提供任何官方适配器，不 import 任何客户端库。

因此注入只有两种形式，都不需要本包知道后端身份。

#### 形式一：接口注入（主路径）

4.2 那组小接口就是全部契约。因为必需方法只有 3 个，用户侧包装任何后端都是几十行的事 —— 下面是**用户代码示例，本包不依赖 redis，也不提供此类型**：

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
		return nil, gcache.ErrCacheMiss // 错误翻译由注入方负责；the injecting side owns error translation.
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

关键点：用户传入的是**已构造好的客户端**，而不是我们自研的 `Options`。用户保有该客户端的全部配置能力（TLS/Cluster/Sentinel/hooks），4.2.4 的有损映射问题自然消失；错误翻译（`redis.Nil` → `gcache.ErrCacheMiss`）也归用户侧，因为只有那一层知道后端的错误约定。

依赖方向天然正确：用户代码 → gcache，绝不反向。这也正是分层原则第 3.2 节"能力层只定义接口，注入发生在用户代码"的字面落实。

#### 形式二：函数注入（无需声明类型）

对于一次性包装、测试替身、或后端能力本就是一堆闭包的场景，提供结构体形式的函数注入，避免用户为了三个方法专门定义类型：

```go
// Funcs 用闭包实现 KV；未提供的字段视为不支持该能力。
// Funcs implements KV via closures; a nil field means the capability is absent.
type Funcs struct {
	GetFunc    func(context.Context, string) ([]byte, error)
	SetFunc    func(context.Context, string, []byte, time.Duration) error
	DeleteFunc func(context.Context, string) error
}
```

#### 明确不做

- **不提供** `gcache/adapters/*` 官方适配器（无论同模块还是独立子模块）。
- **不 import** 任何客户端库，包括测试与示例代码（示例只出现在 README 文本里，不是可编译目标）。
- **不识别后端身份**：包内不存在 `if 是 redis` 之类的分支，能力差异一律走 4.3 的接口断言。

> 与业界做法的对比：最流行的多后端缓存库 eko/gocache 采取"核心与各 store 各自独立发布模块"
> （`github.com/eko/gocache/lib/v4` 与 `github.com/eko/gocache/store/redis/v4` 有各自版本序列），
> 以此避免依赖膨胀。本方案更进一步 —— **连 store 都不收进仓库**，
> 后端归属完全交给用户，仓库因此不承担任何客户端库的版本跟随与 CVE 跟进负担。

#### 本包的真正价值：接口之上的函数层

移除后端后，包的价值从"内置了几种后端"转为"**一套小契约 + 一批与后端无关的函数**"。这些函数对任何注入实现都成立，且零依赖：

- `GetOrLoad`（4.4）—— 未命中加载并写回。
- `Exists`（4.3）—— 原生 EXISTS 优先、否则回退 Get。
- `GetJSON[T]` / `SetJSON[T]` —— 类型化读写。
- 后续可选：命名空间前缀包装、`singleflight` 去重、**多级缓存组合**（自带内存实现作 L1 + 用户注入的远端作 L2）。多级组合是这套设计最自然的收益：它只依赖 `KV` 接口，因此对用户传入的任何后端都能用。

### 4.6 已评估但不采用的方案

| 方案 | 不采用的原因 |
|------|--------------|
| 窄 `Doer` 命令执行器：`Do(ctx, args ...any) (any, error)` | 本包要解析 RESP 回复形状，等于把 Redis 回复约定编进一个声称"不关心后端"的包，且丢失类型安全。`*redis.Client` 也不能直接满足它（其 `Do` 返回 `*redis.Cmd`），用户仍要写 shim —— 不如直接实现 3 个方法的 `KV`。用户若想用一份代码覆盖 Redis/Valkey/Dragonfly/KeyDB/Garnet，可在**自己的**注入层用这种形式。 |
| `database/sql` 式驱动注册表（`Register`+`Open(dsn)`） | 引入全局可变状态与字符串化配置，且注册表意味着本包要认识后端名字 —— 与"不关心传入什么"直接冲突。缓存后端由调用方直接构造更清晰、可测。 |
| 自己用 `net.Conn` 实现 RESP | 等于重写一个客户端（连接池/Cluster/TLS/重试），成熟度倒退，且重新引入本包不该有的网络栈。 |
| 保留 hash/list/set 接口，交由用户实现 | 需要 `HGET` 的场景本质上是"我要用 Redis 数据结构"，此时直接用 Redis 客户端最直接；经缓存抽象转手只会削弱两边。且依据 3.A，绝大多数后端根本无法实现，接口会退化成一堆 `ErrNotSupported` —— 正是本次要消除的现状。 |

### 4.7 用 CI 锁死"零第三方依赖"

扩展 `scripts/check-deps.sh`，新增断言（已实测：对现状**正确失败**并列出 14 个包，对移除后**通过**，无误报）：

```bash
# gcache 必须零第三方依赖（仅 stdlib + 自身）。
# gcache must stay dependency-free (stdlib + itself only).
out="$(go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./gcache/... \
  | grep -v "^$MODULE" || true)"
```

> 注：判定"是否 stdlib"必须用 `go list` 的 `.Standard` 字段，不要用"路径含点"的启发式 ——
> 后者会把 `crypto/internal/entropy/v1.0.0` 误报为第三方。

## 5. 实施阶段

拆成可独立 review 的阶段；每阶段自带验证。

### P0 —— 移除依赖（已在 `/tmp` 副本全程验证）

1. 删除 `gcache/redis.go`、`gcache/valkey.go`、`gcache/redis_test.go`、`gcache/valkey_test.go`。
2. 从 `cache_test.go` 移除 4 个 redis/valkey 相关测试（含 2 个 `translate*Error` 与 2 个 `New_Fail`）。
3. `go mod tidy` —— 预期恰好删除 2.2 节那 4 个模块（**不要**顺带提交 6 个陈旧条目的清理）。
4. 从 `Options` 移除连接类字段（`Address`/`Password`/`DB`/`PoolSize`/`MinIdleConns`/`DialTimeout`/`ReadTimeout`/`WriteTimeout`/`MaxRetries`）及对应 `With*`；保留 `CleanupInterval`。
5. 文档：`gcache/README.md`、`gcache/README.en.md`、根 `README.md:18`（"缓存（内存、Redis、Valkey）"）、`CHANGELOG.md`（`[Unreleased]` 下按 Keep a Changelog 记为破坏性变更）。
6. `scripts/check-deps.sh` 加入 4.7 断言。

P0 完成即拿到全部体积收益，且不改变 `MemoryCache` 现有方法签名 —— 风险最低。

### P1 —— 接口重构

1. 按 4.2 重写 `cache.go` 接口层；删除 `Hash*`/`List*`/`Set*` 接口与 `BasicCache`/`Cache` 大 composite。
2. `MemoryCache` 删除 20 个 `ErrNotSupported` 桩；统一为 ctx 优先单一方法集；`Increment`/`Decrement` → `Add`。
3. `GetOrSet` 方法 → `GetOrLoad` 自由函数（4.4）；`Exists` 降级函数（4.3）。
4. 可选：`GetJSON[T]`/`SetJSON[T]` 泛型辅助，替代裸 `Serializer` 用法。
5. 新增 `gcache/cachetest` 子包：**按能力拆分**的一致性套件（`RunKV` / `RunTTL` / `RunCounter`），彻底取代 `if _, ok := cache.(*MemoryCache)` 开洞。它同时是交付给用户的**契约验证工具** —— 用户注入自己的后端后，可 import 该子包自证实现合规，这是本包不提供适配器之后对用户最实际的支持。该子包只依赖 `testing`（stdlib），不破坏零依赖约束。
6. 两份 README 增补"如何接入自己的后端"：契约说明 + 4.5 的示例（纯文本示例，不作为可编译目标，避免引入依赖）。

### P2 —— 本地缓存统一（已决策：单独 PR，不混入本次）

现状 `MemoryCache` 用 `[]byte`，而 `LRUCache`/`LFUCache`/`TimedCache` 用 `interface{}`；线程安全模型也不一致（`ThreadSafeLRUCache` 独立类型 vs `MemoryCache` 内建锁）。建议泛型化 `Cache[K comparable, V any]` 并统一锁模型。

注意：远端后端天然是字节导向（要过线），而进程内缓存的核心优势正是**免序列化**地存 Go 值。因此不强行让 LRU/LFU 去满足字节导向的 `KV`，两者诚实分开，需要时提供显式桥接。

**P2 必须一并修掉的两处既有数据竞争**（P1 实施时由 `go test -race ./gcache/...` 暴露，与本次改动无关，已决策不混入 P0/P1 的 diff）。CI 的 `go test -race ./...` 门禁在此之前就是红的：

- `lru.go:81` —— `ThreadSafeLRUCache.Get` 取 `RLock`，但底层 `LRUCache.Get` 会 `MoveToFront` 改动链表，并发读实际在写共享状态。同文件的 `ThreadSafeLFUCache.Get` 已用写锁并注明"频率会变更"，LRU 属遗漏；最小修法是改 `Lock`，但统一锁模型时应一并重新设计。
- `timed.go:101` —— `cleanupLoop` 在 `select` 中无锁读 `c.stop`，而 `Close` 持锁写 `c.stop = nil`。停止信号不应依赖"把通道置 nil"来表达。

## 6. 破坏性变更清单

| 变更 | 迁移方式 |
|------|----------|
| 删除 `NewRedisCache` / `NewValkeyCache` | 按 4.5 形式一自行注入（~40 行），或用 `Funcs` 闭包注入 |
| 删除 `RedisCache` / `ValkeyCache` 类型 | 同上；本包不再提供任何远端实现 |
| `Options` 移除 9 个连接字段与 `With*` | 直接配置 `redis.Options` / `valkey.ClientOption`（能力更全） |
| 删除 `*WithContext` 全部孪生方法（P1） | 统一改用 ctx 优先方法；README 示例本就是这个形状 |
| `Increment`/`Decrement` → `Add`（P1） | `Add(ctx,k,n)` / `Add(ctx,k,-n)`，或用 `Incr`/`Decr` 辅助函数 |
| 删除 `Hash*`/`List*`/`Set*`（P1） | 需要 Redis 数据结构者直接用 Redis 客户端 |
| `MemoryCache` 不再返回 `ErrNotSupported`（P1） | 能力改为类型断言判定 |

均属 pre-v1.0.0 允许的破坏性变更，须在 `CHANGELOG.md` 与两份 README 显式标注。

## 7. 决策记录与遗留问题

### 已决策（2026-08-22）

1. **不提供任何官方适配器。** 本包只定义接口与函数，底层实现由用户传入，包不关心传入的是什么。据此删除原 P2"参考适配器"阶段，并在 4.5 增加"明确不做"清单。
2. **接受删除全部 `*WithContext` 孪生方法**，统一 ctx 优先单一方法集（方法数 41 → 约 8）。
3. **本地缓存泛型化单独 PR**，不与依赖移除混在同一 diff。

### 原遗留项的最终定夺（2026-08-22，实施时）

4. **`Exister` 命名保留**，不改 `Presence`，也不并入 `KV`：并入会让无原生 EXISTS 的后端被迫写桩，与 4.3 冲突。
5. **删除 `Serializer` / `JSONSerializer`**，改用 `GetJSON[T]` / `SetJSON[T]`。判据是「包内是否有消费者」：这两个符号在包内无人调用，`JSONSerializer` 也只是 `json.Marshal` 的十行包装；Go 的惯例是这类接口定义在消费方而非生产方。需要非 JSON 编码的用户自行序列化后调 `Set` 即可。
6. **多级缓存组合（L1 内存 + L2 注入）不纳入本次**，属于新增能力而非移除目标。它只依赖 `KV`、零依赖，可在 P0/P1 落地后单独评估。

## 8. 验收标准

- `go build ./...` / `go test ./gcache/...` 通过（已在副本验证：`ok github.com/sofiworker/gk/gcache`）。
- `go list -deps ./gcache` 第三方包数 = **0**。
- `scripts/check-deps.sh` 含 4.7 断言并通过；`make check` 全绿。
- 根 `go.mod` 恰好减少 2.2 节所列 4 个模块，无其他无关增删。
- 复测二进制：只用内存缓存的程序回落到约 1.16x 手写实现（约 2.75 MB）。
- **全仓（含测试、示例）grep 无 `go-redis` / `valkey-go` 残留**，确保"不 import 任何客户端库"这条约束真正成立。
- 文档：两份 gcache README + 根 README + CHANGELOG 同步，破坏性变更显式标注，并写明"远端后端由用户注入，本包不提供"。

## 9. 实施结果（2026-08-22）

P0 与 P1 已落地，全部验收标准通过。

### 落地文件

| 文件 | 变化 |
|------|------|
| `gcache/redis.go`、`gcache/valkey.go`、`gcache/redis_test.go`、`gcache/valkey_test.go` | 删除 |
| `gcache/cache_test.go` | 删除（内容按能力拆分到下列测试文件与 `cachetest`） |
| `gcache/cache.go` | 重写为小接口契约 + 错误 + `Options` |
| `gcache/memory.go` | ctx 优先单一方法集，删除 20 个 `ErrNotSupported` 桩 |
| `gcache/ops.go` | 新增：`GetOrLoad` / `Exists` / `Incr` / `Decr` / `GetJSON[T]` / `SetJSON[T]` |
| `gcache/funcs.go` | 新增：`Funcs` 闭包注入 |
| `gcache/cachetest/cachetest.go` | 新增：`RunKV` / `RunTTL` / `RunCounter` |
| `gcache/memory_test.go`、`ops_test.go`、`funcs_test.go`、`conformance_test.go` | 新增/重写 |
| `go.mod`、`go.sum` | 移除 4 个模块及其 8 行校验和 |
| `scripts/check-deps.sh` | 新增 `DEPENDENCY_FREE_PACKAGES` 断言 |

### 验收数据（本地实测）

- `go build ./...`、`go vet ./...`、`go test ./...` 全绿；`go mod verify` 通过。
- `go list -deps ./gcache/...` 第三方包 = **0**。
- `scripts/check-deps.sh` 通过；植入一个第三方 import 做反向验证时正确失败。
- 二进制复测：gcache probe 2,750,421 B ÷ 手写等价 2,367,594 B = **1.1617x**（预测 1.16x）；相对内置 redis/valkey 的 4,886,751 B 省下 **2,136,330 B = 2.04 MiB = 43.7%**。
- 全仓 grep（含隐藏文件）无 `go-redis` / `valkey-go` 代码残留，仅剩两份 README 的用户代码示例与 CHANGELOG/本文档的说明文字。

### 实施中的额外发现

- `MemoryCache` 原先在 `WithCleanupInterval(0)` 时会 panic（`time.NewTicker` 不接受非正间隔）。已改为不启动后台 goroutine，过期项仍在读取时剔除。
- `go test -race ./gcache/...` 暴露两处**既有**数据竞争，均在本次未改动的本地缓存文件中，属第 5 节 P2「统一锁模型」的范围：
  - `lru.go:81` —— `ThreadSafeLRUCache.Get` 用 `RLock`，但底层 `LRUCache.Get` 会 `MoveToFront` 改链表，并发读实际在写共享状态。同文件的 `ThreadSafeLFUCache.Get` 已用写锁并写明理由，LRU 属遗漏。
  - `timed.go:101` —— `cleanupLoop` 在 `select` 中无锁读 `c.stop`，而 `Close` 持锁写 `c.stop = nil`。

## 10. P2 实施结果（2026-08-22）

本地缓存统一已落地，与 P0/P1 独立成批。锁模型经决策取「内建锁、删除 `ThreadSafe*` 包装类型」。

### 落地改动

| 文件 | 变化 |
|------|------|
| `gcache/lru.go` | 泛型化 `LRUCache[K comparable, V any]`；内建 `sync.Mutex`（`Get` 也取写锁，因其 `MoveToFront`）；删除 `ThreadSafeLRUCache` 及 `NewThreadSafeLRUCache` |
| `gcache/lfu.go` | 泛型化 `LFUCache[K, V]`；内建 `sync.Mutex`；删除 `ThreadSafeLFUCache` 及 `NewThreadSafeLFUCache` |
| `gcache/timed.go` | 泛型化 `TimedCache[K, V]`；保留 `sync.RWMutex`（`Get` 只读）；`Close` 改用 `sync.Once` 幂等关闭固定 `stop` 通道，不再置 nil |
| `gcache/{lru,lfu,timed}_test.go` | 泛型化调用；并发用例改打泛型裸类型；新增 `ConcurrentGetOnly` 直击「`Get` 改共享状态」的竞争点；`TimedCache` 白盒断言（`stop == nil`）改为幂等 `Close` 与行为断言 |

### 两处既有数据竞争的修复

- **LRU**（结构性消除）：`ThreadSafeLRUCache` 用 `RLock` 包 `MoveToFront` 的错误随包装类型删除而消失；泛型 `LRUCache.Get` 直接取写锁。
- **TimedCache**：停止信号不再靠「把通道置 nil」表达 —— `stop` 通道创建后不变，`Close` 用 `sync.Once` 保证只 `close` 一次，`cleanupLoop` 只读该通道，读写竞争消除，`Close` 亦幂等。

### 验收数据（本地实测）

- `go build ./...`、`go vet ./...`、`gofmt -l gcache/` 干净。
- **`go test -race ./gcache/...` 全绿**（P1 时为红）。
- 回归验证：在副本中把 `LRUCache.Get` 的写锁退回 `RLock` 后，新增的 `ConcurrentGetOnly` 在 `-race` 下如期报 `DATA RACE`；恢复写锁后通过 —— 证明该用例确实能捕捉此类回归。
- API 面未擅自扩张：三种缓存维持原有 `Get`/`Set`/`Len`（`TimedCache` 另有 `Delete`/`Close`），未新增计划外方法。

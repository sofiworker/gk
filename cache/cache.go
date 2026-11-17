// Package gcache 提供进程内缓存实现，以及一组与具体后端无关的缓存契约和工具函数。
// Package gcache provides an in-process cache plus backend-agnostic cache contracts and helpers.
//
// 本包不 import 任何客户端库，也不识别具体后端。远端缓存由用户实现下面的接口后传入，
// 依赖方向永远是「用户代码 → gcache」，因此只用内存缓存的程序不会链入任何网络栈。
// It imports no client library and recognises no concrete backend. Remote caches are injected by
// users implementing the interfaces below, so the dependency direction is always user code → gcache
// and a memory-only program links in no networking stack.
package gcache

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrCacheMiss 表示键不存在或已过期。注入方需要把后端的「未命中」错误翻译成它。
	// ErrCacheMiss reports an absent or expired key; the injecting side translates the
	// backend's own miss error into it.
	ErrCacheMiss = errors.New("gcache: cache miss")

	// ErrNilLoader 表示未命中时调用方没有提供 loader。
	// ErrNilLoader reports that the caller supplied no loader for a miss.
	ErrNilLoader = errors.New("gcache: loader is nil")

	// ErrNotImplemented 表示注入的实现缺少该操作，例如 Funcs 的对应字段为 nil。
	// ErrNotImplemented reports that the injected implementation lacks the operation,
	// e.g. a nil field in Funcs.
	ErrNotImplemented = errors.New("gcache: operation not implemented")
)

// Getter 读取一个键；未命中必须返回 ErrCacheMiss。
// Getter reads one key and must return ErrCacheMiss when the key is absent.
type Getter interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

// Setter 写入一个键，ttl <= 0 表示不过期。
// Setter writes one key; a ttl <= 0 means no expiration.
type Setter interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

// Deleter 删除一个键。删除不存在的键是幂等操作，不得返回错误。
// Deleter removes one key. Deleting an absent key is idempotent and must not error.
type Deleter interface {
	Delete(ctx context.Context, key string) error
}

// KV 是后端必须满足的唯一契约，其余能力一律可选。
// KV is the only contract a backend must satisfy; every other capability is optional.
type KV interface {
	Getter
	Setter
	Deleter
}

// Exister 由具备原生存在性判断的后端实现；没有实现时用 Exists 函数回退到 Get。
// Exister is implemented by backends with a native existence check; otherwise the
// Exists helper falls back to Get.
type Exister interface {
	Exists(ctx context.Context, key string) (bool, error)
}

// TTLReader 读取键的剩余生存时间：负值表示永不过期，键不存在时返回 ErrCacheMiss。
// TTLReader reads a key's remaining lifetime: a negative value means no expiration,
// and an absent key yields ErrCacheMiss.
type TTLReader interface {
	TTL(ctx context.Context, key string) (time.Duration, error)
}

// Expirer 为已存在的键重设过期时间；键不存在时返回 ErrCacheMiss。
// 与 Setter 保持一致，ttl <= 0 表示清除过期时间（键转为永不过期），而不是立即删除。
// Expirer resets the expiration of an existing key and returns ErrCacheMiss if it is absent.
// Consistently with Setter, a ttl <= 0 clears the expiration (the key becomes permanent)
// rather than deleting the key.
type Expirer interface {
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// Counter 以 delta 原子增减并返回新值，delta 为负即递减。
// Counter atomically adds delta and returns the new value; a negative delta decrements.
type Counter interface {
	Add(ctx context.Context, key string, delta int64) (int64, error)
}

// Pinger 探测后端可用性。
// Pinger probes backend availability.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Options 保存进程内缓存的配置。远端连接参数属于用户自行构造的客户端，
// 刻意不在此处表达，以免复刻一份永远追不上上游的有损配置面。
// Options holds in-process cache configuration. Remote connection settings belong to the
// user-constructed client and are deliberately absent, so the package never mirrors a lossy
// subset of an upstream client's configuration surface.
type Options struct {
	CleanupInterval time.Duration
}

// Option configures an Options struct.
type Option func(*Options)

// WithCleanupInterval 设置 MemoryCache 清理过期项的间隔；非正值表示关闭后台清理。
// WithCleanupInterval sets how often MemoryCache reaps expired items; a non-positive
// value disables background cleanup.
func WithCleanupInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.CleanupInterval = interval
	}
}

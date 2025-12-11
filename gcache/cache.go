package gcache

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCacheMiss = errors.New("gcache: cache miss")
)

// KeyValueCacheWithContext defines the interface for key-value cache operations with context.
type KeyValueCacheWithContext interface {
	GetWithContext(ctx context.Context, key string) ([]byte, error)
	SetWithContext(ctx context.Context, key string, value []byte, expiration time.Duration) error
	DeleteWithContext(ctx context.Context, key string) error
	ExistsWithContext(ctx context.Context, key string) (bool, error)
}

// KeyValueCache defines the interface for key-value cache operations.
type KeyValueCache interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, expiration time.Duration) error
	Delete(key string) error
	Exists(key string) (bool, error)
}

// ExpirableCacheWithContext defines the interface for cache expiration operations with context.
type ExpirableCacheWithContext interface {
	ExpireWithContext(ctx context.Context, key string, expiration time.Duration) error
	TTLWithContext(ctx context.Context, key string) (time.Duration, error)
}

// ExpirableCache defines the interface for cache expiration operations.
type ExpirableCache interface {
	Expire(key string, expiration time.Duration) error
	TTL(key string) (time.Duration, error)
}

// CounterCacheWithContext defines the interface for counter operations with context.
type CounterCacheWithContext interface {
	IncrementWithContext(ctx context.Context, key string, value int64) (int64, error)
	DecrementWithContext(ctx context.Context, key string, value int64) (int64, error)
}

// CounterCache defines the interface for counter operations.
type CounterCache interface {
	Increment(key string, value int64) (int64, error)
	Decrement(key string, value int64) (int64, error)
}

// HashCacheWithContext defines the interface for hash operations with context.
type HashCacheWithContext interface {
	HashSetWithContext(ctx context.Context, key string, field string, value []byte) error
	HashGetWithContext(ctx context.Context, key string, field string) ([]byte, error)
	HashGetAllWithContext(ctx context.Context, key string) (map[string][]byte, error)
	HashDeleteWithContext(ctx context.Context, key string, fields ...string) error
}

// HashCache defines the interface for hash operations.
type HashCache interface {
	HashSet(key string, field string, value []byte) error
	HashGet(key string, field string) ([]byte, error)
	HashGetAll(key string) (map[string][]byte, error)
	HashDelete(key string, fields ...string) error
}

// ListCacheWithContext defines the interface for list operations with context.
type ListCacheWithContext interface {
	ListPushWithContext(ctx context.Context, key string, values ...[]byte) error
	ListPopWithContext(ctx context.Context, key string) ([]byte, error)
	ListRangeWithContext(ctx context.Context, key string, start, stop int64) ([][]byte, error)
}

// ListCache defines the interface for list operations.
type ListCache interface {
	ListPush(key string, values ...[]byte) error
	ListPop(key string) ([]byte, error)
	ListRange(key string, start, stop int64) ([][]byte, error)
}

// SetCacheWithContext defines the interface for set operations with context.
type SetCacheWithContext interface {
	SetAddWithContext(ctx context.Context, key string, members ...[]byte) error
	SetMembersWithContext(ctx context.Context, key string) ([][]byte, error)
	SetIsMemberWithContext(ctx context.Context, key string, member []byte) (bool, error)
}

// SetCache defines the interface for set operations.
type SetCache interface {
	SetAdd(key string, members ...[]byte) error
	SetMembers(key string) ([][]byte, error)
	SetIsMember(key string, member []byte) (bool, error)
}

// HealthCheckerWithContext defines the interface for health check operations with context.
type HealthCheckerWithContext interface {
	PingWithContext(ctx context.Context) error
}

// HealthChecker defines the interface for health check operations.
type HealthChecker interface {
	Ping() error
}

// Closer defines the interface for closing a resource.
type Closer interface {
	Close() error
}

// BasicCacheWithContext is a composite interface for basic cache operations with context.
type BasicCacheWithContext interface {
	KeyValueCacheWithContext
	ExpirableCacheWithContext
	CounterCacheWithContext
	HealthCheckerWithContext
	Closer
}

// BasicCache is a composite interface for basic cache operations.
type BasicCache interface {
	KeyValueCache
	ExpirableCache
	CounterCache
	HealthChecker
	Closer
}

// CacheWithContext is a composite interface for all cache operations with context.
type CacheWithContext interface {
	BasicCacheWithContext
	HashCacheWithContext
	ListCacheWithContext
	SetCacheWithContext
}

// Cache is a composite interface for all cache operations.
type Cache interface {
	BasicCache
	HashCache
	ListCache
	SetCache
}

// Options holds configuration for cache clients.
type Options struct {
	Address         string
	Password        string
	DB              int
	PoolSize        int
	MinIdleConns    int
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxRetries      int
	CleanupInterval time.Duration // For MemoryCache
}

// Option configures an Options struct.
type Option func(*Options)

// WithAddress sets the network address.
func WithAddress(address string) Option {
	return func(o *Options) {
		o.Address = address
	}
}

// WithPassword sets the password.
func WithPassword(password string) Option {
	return func(o *Options) {
		o.Password = password
	}
}

// WithDB sets the database index.
func WithDB(db int) Option {
	return func(o *Options) {
		o.DB = db
	}
}

// WithPoolSize sets the connection pool size.
func WithPoolSize(poolSize int) Option {
	return func(o *Options) {
		o.PoolSize = poolSize
	}
}

// WithMinIdleConns sets the minimum number of idle connections.
func WithMinIdleConns(minIdleConns int) Option {
	return func(o *Options) {
		o.MinIdleConns = minIdleConns
	}
}

// WithDialTimeout sets the dial timeout.
func WithDialTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.DialTimeout = timeout
	}
}

// WithReadTimeout sets the read timeout.
func WithReadTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.ReadTimeout = timeout
	}
}

// WithWriteTimeout sets the write timeout.
func WithWriteTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.WriteTimeout = timeout
	}
}

// WithMaxRetries sets the maximum number of retries.
func WithMaxRetries(maxRetries int) Option {
	return func(o *Options) {
		o.MaxRetries = maxRetries
	}
}

// WithCleanupInterval sets the cleanup interval for expired items in MemoryCache.
func WithCleanupInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.CleanupInterval = interval
	}
}

type Serializer interface {
	Serialize(v interface{}) ([]byte, error)
	Deserialize(data []byte, v interface{}) error
}

type JSONSerializer struct{}

func (j JSONSerializer) Serialize(v interface{}) ([]byte, error) {
	return []byte{}, nil
}

func (j JSONSerializer) Deserialize(data []byte, v interface{}) error {
	return nil
}

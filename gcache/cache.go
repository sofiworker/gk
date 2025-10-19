package gcache

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrCacheMiss 表示缓存未命中
	ErrCacheMiss = errors.New("gcache: cache miss")
)

// KeyValueCache 负责基础的 KV 能力
type KeyValueCache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, expiration time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// ExpirableCache 负责过期时间管理
type ExpirableCache interface {
	Expire(ctx context.Context, key string, expiration time.Duration) error
	TTL(ctx context.Context, key string) (time.Duration, error)
}

// CounterCache 负责数值自增自减
type CounterCache interface {
	Increment(ctx context.Context, key string, value int64) (int64, error)
	Decrement(ctx context.Context, key string, value int64) (int64, error)
}

// HashCache 提供 Hash 结构能力
type HashCache interface {
	HashSet(ctx context.Context, key string, field string, value []byte) error
	HashGet(ctx context.Context, key string, field string) ([]byte, error)
	HashGetAll(ctx context.Context, key string) (map[string][]byte, error)
	HashDelete(ctx context.Context, key string, fields ...string) error
}

// ListCache 提供列表能力
type ListCache interface {
	ListPush(ctx context.Context, key string, values ...[]byte) error
	ListPop(ctx context.Context, key string) ([]byte, error)
	ListRange(ctx context.Context, key string, start, stop int64) ([][]byte, error)
}

// SetCache 提供集合能力
type SetCache interface {
	SetAdd(ctx context.Context, key string, members ...[]byte) error
	SetMembers(ctx context.Context, key string) ([][]byte, error)
	SetIsMember(ctx context.Context, key string, member []byte) (bool, error)
}

// HealthChecker 用于探活
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// Closer 用于资源释放
type Closer interface {
	Close() error
}

// BasicCache 组合 KV、过期、计数和健康检查能力
type BasicCache interface {
	KeyValueCache
	ExpirableCache
	CounterCache
	HealthChecker
	Closer
}

// Cache 在 Basic 基础上组合 Hash/List/Set 能力，适用于 Redis 等
type Cache interface {
	BasicCache
	HashCache
	ListCache
	SetCache
}

// Options 缓存配置选项
type Options struct {
	// 连接地址
	Address string
	// 密码
	Password string
	// 数据库
	DB int
	// 连接池大小
	PoolSize int
	// 最小空闲连接数
	MinIdleConns int
	// 连接超时
	DialTimeout time.Duration
	// 读取超时
	ReadTimeout time.Duration
	// 写入超时
	WriteTimeout time.Duration
	// 最大重试次数
	MaxRetries int
}

// Serializer 序列化接口
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

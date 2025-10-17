package gcache

import (
	"context"
	"time"
)

// Cache 通用缓存接口
type Cache interface {
	// Get 获取缓存值
	Get(ctx context.Context, key string) ([]byte, error)

	// Set 设置缓存值
	Set(ctx context.Context, key string, value []byte, expiration time.Duration) error

	// Delete 删除缓存
	Delete(ctx context.Context, key string) error

	// Exists 检查key是否存在
	Exists(ctx context.Context, key string) (bool, error)

	// Expire 设置过期时间
	Expire(ctx context.Context, key string, expiration time.Duration) error

	// TTL 获取剩余过期时间
	TTL(ctx context.Context, key string) (time.Duration, error)

	// Increment 自增
	Increment(ctx context.Context, key string, value int64) (int64, error)

	// Decrement 自减
	Decrement(ctx context.Context, key string, value int64) (int64, error)

	// HashSet 设置hash字段
	HashSet(ctx context.Context, key string, field string, value []byte) error

	// HashGet 获取hash字段
	HashGet(ctx context.Context, key string, field string) ([]byte, error)

	// HashGetAll 获取所有hash字段
	HashGetAll(ctx context.Context, key string) (map[string][]byte, error)

	// HashDelete 删除hash字段
	HashDelete(ctx context.Context, key string, fields ...string) error

	// ListPush 列表推送
	ListPush(ctx context.Context, key string, values ...[]byte) error

	// ListPop 列表弹出
	ListPop(ctx context.Context, key string) ([]byte, error)

	// ListRange 获取列表范围
	ListRange(ctx context.Context, key string, start, stop int64) ([][]byte, error)

	// SetAdd 集合添加
	SetAdd(ctx context.Context, key string, members ...[]byte) error

	// SetMembers 获取集合所有成员
	SetMembers(ctx context.Context, key string) ([][]byte, error)

	// SetIsMember 判断是否集合成员
	SetIsMember(ctx context.Context, key string, member []byte) (bool, error)

	// Close 关闭连接
	Close() error

	// Ping 测试连接
	Ping(ctx context.Context) error
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

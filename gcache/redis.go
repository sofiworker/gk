package gcache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache Redis实现
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache 创建Redis缓存实例
func NewRedisCache(opts *Options) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         opts.Address,
		Password:     opts.Password,
		DB:           opts.DB,
		PoolSize:     opts.PoolSize,
		MinIdleConns: opts.MinIdleConns,
		DialTimeout:  opts.DialTimeout,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		MaxRetries:   opts.MaxRetries,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisCache{client: client}, nil
}

func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	return r.client.Get(ctx, key).Bytes()
}

func (r *RedisCache) Set(ctx context.Context, key string, value []byte, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *RedisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	result, err := r.client.Exists(ctx, key).Result()
	return result > 0, err
}

func (r *RedisCache) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return r.client.Expire(ctx, key, expiration).Err()
}

func (r *RedisCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}

func (r *RedisCache) Increment(ctx context.Context, key string, value int64) (int64, error) {
	return r.client.IncrBy(ctx, key, value).Result()
}

func (r *RedisCache) Decrement(ctx context.Context, key string, value int64) (int64, error) {
	return r.client.DecrBy(ctx, key, value).Result()
}

func (r *RedisCache) HashSet(ctx context.Context, key string, field string, value []byte) error {
	return r.client.HSet(ctx, key, field, value).Err()
}

func (r *RedisCache) HashGet(ctx context.Context, key string, field string) ([]byte, error) {
	return r.client.HGet(ctx, key, field).Bytes()
}

func (r *RedisCache) HashGetAll(ctx context.Context, key string) (map[string][]byte, error) {
	result, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	data := make(map[string][]byte)
	for k, v := range result {
		data[k] = []byte(v)
	}
	return data, nil
}

func (r *RedisCache) HashDelete(ctx context.Context, key string, fields ...string) error {
	return r.client.HDel(ctx, key, fields...).Err()
}

func (r *RedisCache) ListPush(ctx context.Context, key string, values ...[]byte) error {
	interfaceValues := make([]interface{}, len(values))
	for i, v := range values {
		interfaceValues[i] = v
	}
	return r.client.RPush(ctx, key, interfaceValues...).Err()
}

func (r *RedisCache) ListPop(ctx context.Context, key string) ([]byte, error) {
	return r.client.LPop(ctx, key).Bytes()
}

func (r *RedisCache) ListRange(ctx context.Context, key string, start, stop int64) ([][]byte, error) {
	result, err := r.client.LRange(ctx, key, start, stop).Result()
	if err != nil {
		return nil, err
	}

	data := make([][]byte, len(result))
	for i, v := range result {
		data[i] = []byte(v)
	}
	return data, nil
}

func (r *RedisCache) SetAdd(ctx context.Context, key string, members ...[]byte) error {
	interfaceMembers := make([]interface{}, len(members))
	for i, m := range members {
		interfaceMembers[i] = m
	}
	return r.client.SAdd(ctx, key, interfaceMembers...).Err()
}

func (r *RedisCache) SetMembers(ctx context.Context, key string) ([][]byte, error) {
	result, err := r.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	data := make([][]byte, len(result))
	for i, v := range result {
		data[i] = []byte(v)
	}
	return data, nil
}

func (r *RedisCache) SetIsMember(ctx context.Context, key string, member []byte) (bool, error) {
	return r.client.SIsMember(ctx, key, member).Result()
}

func (r *RedisCache) Close() error {
	return r.client.Close()
}

func (r *RedisCache) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

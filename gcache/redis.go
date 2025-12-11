package gcache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(opts ...Option) (*RedisCache, error) {
	options := &Options{}
	for _, o := range opts {
		o(options)
	}

	opt := &redis.Options{
		Addr:         options.Address,
		Password:     options.Password,
		DB:           options.DB,
		PoolSize:     options.PoolSize,
		MinIdleConns: options.MinIdleConns,
		DialTimeout:  options.DialTimeout,
		ReadTimeout:  options.ReadTimeout,
		WriteTimeout: options.WriteTimeout,
		MaxRetries:   options.MaxRetries,
	}
	client := redis.NewClient(opt)
	cache := &RedisCache{client: client}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cache.PingWithContext(ctx); err != nil {
		return nil, err
	}

	return cache, nil
}

func (r *RedisCache) GetWithContext(ctx context.Context, key string) ([]byte, error) {
	result, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, translateRedisError(err)
	}
	return result, nil
}

func (r *RedisCache) Get(key string) ([]byte, error) {
	return r.GetWithContext(context.Background(), key)
}

func (r *RedisCache) SetWithContext(ctx context.Context, key string, value []byte, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *RedisCache) Set(key string, value []byte, expiration time.Duration) error {
	return r.SetWithContext(context.Background(), key, value, expiration)
}

func (r *RedisCache) DeleteWithContext(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *RedisCache) Delete(key string) error {
	return r.DeleteWithContext(context.Background(), key)
}

func (r *RedisCache) ExistsWithContext(ctx context.Context, key string) (bool, error) {
	result, err := r.client.Exists(ctx, key).Result()
	return result > 0, err
}

func (r *RedisCache) Exists(key string) (bool, error) {
	return r.ExistsWithContext(context.Background(), key)
}

func (r *RedisCache) ExpireWithContext(ctx context.Context, key string, expiration time.Duration) error {
	return r.client.Expire(ctx, key, expiration).Err()
}

func (r *RedisCache) Expire(key string, expiration time.Duration) error {
	return r.ExpireWithContext(context.Background(), key, expiration)
}

func (r *RedisCache) TTLWithContext(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}

func (r *RedisCache) TTL(key string) (time.Duration, error) {
	return r.TTLWithContext(context.Background(), key)
}

func (r *RedisCache) IncrementWithContext(ctx context.Context, key string, value int64) (int64, error) {
	result, err := r.client.IncrBy(ctx, key, value).Result()
	return result, translateRedisError(err)
}

func (r *RedisCache) Increment(key string, value int64) (int64, error) {
	return r.IncrementWithContext(context.Background(), key, value)
}

func (r *RedisCache) DecrementWithContext(ctx context.Context, key string, value int64) (int64, error) {
	result, err := r.client.DecrBy(ctx, key, value).Result()
	return result, translateRedisError(err)
}

func (r *RedisCache) Decrement(key string, value int64) (int64, error) {
	return r.DecrementWithContext(context.Background(), key, value)
}

func (r *RedisCache) HashSetWithContext(ctx context.Context, key string, field string, value []byte) error {
	return r.client.HSet(ctx, key, field, value).Err()
}

func (r *RedisCache) HashSet(key string, field string, value []byte) error {
	return r.HashSetWithContext(context.Background(), key, field, value)
}

func (r *RedisCache) HashGetWithContext(ctx context.Context, key string, field string) ([]byte, error) {
	result, err := r.client.HGet(ctx, key, field).Bytes()
	if err != nil {
		return nil, translateRedisError(err)
	}
	return result, nil
}

func (r *RedisCache) HashGet(key string, field string) ([]byte, error) {
	return r.HashGetWithContext(context.Background(), key, field)
}

func (r *RedisCache) HashGetAllWithContext(ctx context.Context, key string) (map[string][]byte, error) {
	result, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, translateRedisError(err)
	}

	data := make(map[string][]byte)
	for k, v := range result {
		data[k] = []byte(v)
	}
	return data, nil
}

func (r *RedisCache) HashGetAll(key string) (map[string][]byte, error) {
	return r.HashGetAllWithContext(context.Background(), key)
}

func (r *RedisCache) HashDeleteWithContext(ctx context.Context, key string, fields ...string) error {
	return r.client.HDel(ctx, key, fields...).Err()
}

func (r *RedisCache) HashDelete(key string, fields ...string) error {
	return r.HashDeleteWithContext(context.Background(), key, fields...)
}

func (r *RedisCache) ListPushWithContext(ctx context.Context, key string, values ...[]byte) error {
	interfaceValues := make([]interface{}, len(values))
	for i, v := range values {
		interfaceValues[i] = v
	}
	return r.client.RPush(ctx, key, interfaceValues...).Err()
}

func (r *RedisCache) ListPush(key string, values ...[]byte) error {
	return r.ListPushWithContext(context.Background(), key, values...)
}

func (r *RedisCache) ListPopWithContext(ctx context.Context, key string) ([]byte, error) {
	result, err := r.client.LPop(ctx, key).Bytes()
	if err != nil {
		return nil, translateRedisError(err)
	}
	return result, nil
}

func (r *RedisCache) ListPop(key string) ([]byte, error) {
	return r.ListPopWithContext(context.Background(), key)
}

func (r *RedisCache) ListRangeWithContext(ctx context.Context, key string, start, stop int64) ([][]byte, error) {
	result, err := r.client.LRange(ctx, key, start, stop).Result()
	if err != nil {
		return nil, translateRedisError(err)
	}

	data := make([][]byte, len(result))
	for i, v := range result {
		data[i] = []byte(v)
	}
	return data, nil
}

func (r *RedisCache) ListRange(key string, start, stop int64) ([][]byte, error) {
	return r.ListRangeWithContext(context.Background(), key, start, stop)
}

func (r *RedisCache) SetAddWithContext(ctx context.Context, key string, members ...[]byte) error {
	interfaceMembers := make([]interface{}, len(members))
	for i, m := range members {
		interfaceMembers[i] = m
	}
	return r.client.SAdd(ctx, key, interfaceMembers...).Err()
}

func (r *RedisCache) SetAdd(key string, members ...[]byte) error {
	return r.SetAddWithContext(context.Background(), key, members...)
}

func (r *RedisCache) SetMembersWithContext(ctx context.Context, key string) ([][]byte, error) {
	result, err := r.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, translateRedisError(err)
	}

	data := make([][]byte, len(result))
	for i, v := range result {
		data[i] = []byte(v)
	}
	return data, nil
}

func (r *RedisCache) SetMembers(key string) ([][]byte, error) {
	return r.SetMembersWithContext(context.Background(), key)
}

func (r *RedisCache) SetIsMemberWithContext(ctx context.Context, key string, member []byte) (bool, error) {
	result, err := r.client.SIsMember(ctx, key, member).Result()
	return result, translateRedisError(err)
}

func (r *RedisCache) SetIsMember(key string, member []byte) (bool, error) {
	return r.SetIsMemberWithContext(context.Background(), key, member)
}

func (r *RedisCache) Close() error {
	return r.client.Close()
}

func (r *RedisCache) PingWithContext(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisCache) Ping() error {
	return r.PingWithContext(context.Background())
}

func translateRedisError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, redis.Nil) {
		return ErrCacheMiss
	}
	return err
}

var (
	_ Cache            = (*RedisCache)(nil)
	_ CacheWithContext = (*RedisCache)(nil)
)

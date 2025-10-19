package gcache

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
)

// ValkeyCache Valkey实现
type ValkeyCache struct {
	client valkey.Client
}

// NewValkeyCache 创建Valkey缓存实例
func NewValkeyCache(opts *Options) (*ValkeyCache, error) {
	client, err := valkey.NewClient(valkey.MustParseURL(opts.Address))
	if err != nil {
		return nil, err
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		return nil, err
	}

	return &ValkeyCache{client: client}, nil
}

func (v *ValkeyCache) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := v.client.Do(ctx, v.client.B().Get().Key(key).Build()).AsBytes()
	if err != nil {
		return nil, translateValkeyError(err)
	}
	return data, nil
}

func (v *ValkeyCache) Set(ctx context.Context, key string, value []byte, expiration time.Duration) error {
	return v.client.Do(ctx, v.client.B().Set().Key(key).Value(string(value)).Ex(expiration).Build()).Error()
}

func (v *ValkeyCache) Delete(ctx context.Context, key string) error {
	return v.client.Do(ctx, v.client.B().Del().Key(key).Build()).Error()
}

func (v *ValkeyCache) Exists(ctx context.Context, key string) (bool, error) {
	result, err := v.client.Do(ctx, v.client.B().Exists().Key(key).Build()).AsInt64()
	return result > 0, err
}

func (v *ValkeyCache) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return v.client.Do(ctx, v.client.B().Expire().Key(key).Seconds(int64(expiration.Seconds())).Build()).Error()
}

func (v *ValkeyCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	result, err := v.client.Do(ctx, v.client.B().Ttl().Key(key).Build()).AsInt64()
	return time.Duration(result) * time.Second, err
}

func (v *ValkeyCache) Increment(ctx context.Context, key string, value int64) (int64, error) {
	result, err := v.client.Do(ctx, v.client.B().Incrby().Key(key).Increment(value).Build()).AsInt64()
	return result, translateValkeyError(err)
}

func (v *ValkeyCache) Decrement(ctx context.Context, key string, value int64) (int64, error) {
	result, err := v.client.Do(ctx, v.client.B().Decrby().Key(key).Decrement(value).Build()).AsInt64()
	return result, translateValkeyError(err)
}

func (v *ValkeyCache) HashSet(ctx context.Context, key string, field string, value []byte) error {
	return v.client.Do(ctx, v.client.B().Hset().Key(key).FieldValue().FieldValue(field, string(value)).Build()).Error()
}

func (v *ValkeyCache) HashGet(ctx context.Context, key string, field string) ([]byte, error) {
	result, err := v.client.Do(ctx, v.client.B().Hget().Key(key).Field(field).Build()).AsBytes()
	if err != nil {
		return nil, translateValkeyError(err)
	}
	return result, nil
}

func (v *ValkeyCache) HashGetAll(ctx context.Context, key string) (map[string][]byte, error) {
	result, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, translateValkeyError(err)
	}

	data := make(map[string][]byte)
	for k, v := range result {
		data[k] = []byte(v)
	}
	return data, nil
}

func (v *ValkeyCache) HashDelete(ctx context.Context, key string, fields ...string) error {
	return v.client.Do(ctx, v.client.B().Hdel().Key(key).Field(fields...).Build()).Error()
}

func (v *ValkeyCache) ListPush(ctx context.Context, key string, values ...[]byte) error {
	args := make([]string, len(values))
	for i, v := range values {
		args[i] = string(v)
	}
	return v.client.Do(ctx, v.client.B().Rpush().Key(key).Element(args...).Build()).Error()
}

func (v *ValkeyCache) ListPop(ctx context.Context, key string) ([]byte, error) {
	result, err := v.client.Do(ctx, v.client.B().Lpop().Key(key).Build()).AsBytes()
	if err != nil {
		return nil, translateValkeyError(err)
	}
	return result, nil
}

func (v *ValkeyCache) ListRange(ctx context.Context, key string, start, stop int64) ([][]byte, error) {
	result, err := v.client.Do(ctx, v.client.B().Lrange().Key(key).Start(start).Stop(stop).Build()).AsStrSlice()
	if err != nil {
		return nil, translateValkeyError(err)
	}

	data := make([][]byte, len(result))
	for i, v := range result {
		data[i] = []byte(v)
	}
	return data, nil
}

func (v *ValkeyCache) SetAdd(ctx context.Context, key string, members ...[]byte) error {
	args := make([]string, len(members))
	for i, m := range members {
		args[i] = string(m)
	}
	return v.client.Do(ctx, v.client.B().Sadd().Key(key).Member(args...).Build()).Error()
}

func (v *ValkeyCache) SetMembers(ctx context.Context, key string) ([][]byte, error) {
	result, err := v.client.Do(ctx, v.client.B().Smembers().Key(key).Build()).AsStrSlice()
	if err != nil {
		return nil, translateValkeyError(err)
	}

	data := make([][]byte, len(result))
	for i, v := range result {
		data[i] = []byte(v)
	}
	return data, nil
}

func (v *ValkeyCache) SetIsMember(ctx context.Context, key string, member []byte) (bool, error) {
	result, err := v.client.Do(ctx, v.client.B().Sismember().Key(key).Member(string(member)).Build()).AsInt64()
	if err != nil {
		return false, translateValkeyError(err)
	}
	return result == 1, nil
}

func (v *ValkeyCache) Close() error {
	v.client.Close()
	return nil
}

func (v *ValkeyCache) Ping(ctx context.Context) error {
	return v.client.Do(ctx, v.client.B().Ping().Build()).Error()
}

func translateValkeyError(err error) error {
	if err == nil {
		return nil
	}
	if valkey.IsValkeyNil(err) {
		return ErrCacheMiss
	}
	return err
}

var (
	_ Cache = (*ValkeyCache)(nil)
)

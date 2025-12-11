package gcache

import (
	"context"
	"net"
	"time"

	"github.com/valkey-io/valkey-go"
)

type ValkeyCache struct {
	client valkey.Client
}

func NewValkeyCache(opts ...Option) (*ValkeyCache, error) {
	options := &Options{}
	for _, o := range opts {
		o(options)
	}

	vopt := valkey.ClientOption{
		InitAddress:         []string{options.Address},
		Password:            options.Password,
		SelectDB:            options.DB,
		BlockingPoolSize:    options.PoolSize,
		BlockingPoolMinSize: options.MinIdleConns,
		Dialer: net.Dialer{
			Timeout: options.DialTimeout,
		},
		ConnWriteTimeout: options.WriteTimeout,
		DisableRetry:     options.MaxRetries <= 0,
	}

	client, err := valkey.NewClient(vopt)
	if err != nil {
		return nil, err
	}

	cache := &ValkeyCache{client: client}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cache.PingWithContext(ctx); err != nil {
		return nil, err
	}

	return cache, nil
}

// GetWithContext retrieves data from the cache with a context.
func (v *ValkeyCache) GetWithContext(ctx context.Context, key string) ([]byte, error) {
	data, err := v.client.Do(ctx, v.client.B().Get().Key(key).Build()).AsBytes()
	if err != nil {
		return nil, translateValkeyError(err)
	}
	return data, nil
}

// Get retrieves data from the cache using context.Background().
func (v *ValkeyCache) Get(key string) ([]byte, error) {
	return v.GetWithContext(context.Background(), key)
}

// SetWithContext sets data in the cache with a context.
func (v *ValkeyCache) SetWithContext(ctx context.Context, key string, value []byte, expiration time.Duration) error {
	return v.client.Do(ctx, v.client.B().Set().Key(key).Value(string(value)).Ex(expiration).Build()).Error()
}

// Set sets data in the cache using context.Background().
func (v *ValkeyCache) Set(key string, value []byte, expiration time.Duration) error {
	return v.SetWithContext(context.Background(), key, value, expiration)
}

// DeleteWithContext deletes data from the cache with a context.
func (v *ValkeyCache) DeleteWithContext(ctx context.Context, key string) error {
	return v.client.Do(ctx, v.client.B().Del().Key(key).Build()).Error()
}

// Delete deletes data from the cache using context.Background().
func (v *ValkeyCache) Delete(key string) error {
	return v.DeleteWithContext(context.Background(), key)
}

// ExistsWithContext checks if a key exists in the cache with a context.
func (v *ValkeyCache) ExistsWithContext(ctx context.Context, key string) (bool, error) {
	result, err := v.client.Do(ctx, v.client.B().Exists().Key(key).Build()).AsInt64()
	return result > 0, err
}

// Exists checks if a key exists in the cache using context.Background().
func (v *ValkeyCache) Exists(key string) (bool, error) {
	return v.ExistsWithContext(context.Background(), key)
}

// ExpireWithContext sets the expiration for a key with a context.
func (v *ValkeyCache) ExpireWithContext(ctx context.Context, key string, expiration time.Duration) error {
	return v.client.Do(ctx, v.client.B().Expire().Key(key).Seconds(int64(expiration.Seconds())).Build()).Error()
}

// Expire sets the expiration for a key using context.Background().
func (v *ValkeyCache) Expire(key string, expiration time.Duration) error {
	return v.ExpireWithContext(context.Background(), key, expiration)
}

// TTLWithContext gets the time to live for a key with a context.
func (v *ValkeyCache) TTLWithContext(ctx context.Context, key string) (time.Duration, error) {
	result, err := v.client.Do(ctx, v.client.B().Ttl().Key(key).Build()).AsInt64()
	return time.Duration(result) * time.Second, err
}

// TTL gets the time to live for a key using context.Background().
func (v *ValkeyCache) TTL(key string) (time.Duration, error) {
	return v.TTLWithContext(context.Background(), key)
}

// IncrementWithContext increments the integer value of a key by the given amount with a context.
func (v *ValkeyCache) IncrementWithContext(ctx context.Context, key string, value int64) (int64, error) {
	result, err := v.client.Do(ctx, v.client.B().Incrby().Key(key).Increment(value).Build()).AsInt64()
	return result, translateValkeyError(err)
}

// Increment increments the integer value of a key by the given amount using context.Background().
func (v *ValkeyCache) Increment(key string, value int64) (int64, error) {
	return v.IncrementWithContext(context.Background(), key, value)
}

// DecrementWithContext decrements the integer value of a key by the given amount with a context.
func (v *ValkeyCache) DecrementWithContext(ctx context.Context, key string, value int64) (int64, error) {
	result, err := v.client.Do(ctx, v.client.B().Decrby().Key(key).Decrement(value).Build()).AsInt64()
	return result, translateValkeyError(err)
}

// Decrement decrements the integer value of a key by the given amount using context.Background().
func (v *ValkeyCache) Decrement(key string, value int64) (int64, error) {
	return v.DecrementWithContext(context.Background(), key, value)
}

// HashSetWithContext sets the string value of a hash field with a context.
func (v *ValkeyCache) HashSetWithContext(ctx context.Context, key string, field string, value []byte) error {
	return v.client.Do(ctx, v.client.B().Hset().Key(key).FieldValue().FieldValue(field, string(value)).Build()).Error()
}

// HashSet sets the string value of a hash field using context.Background().
func (v *ValkeyCache) HashSet(key string, field string, value []byte) error {
	return v.HashSetWithContext(context.Background(), key, field, value)
}

// HashGetWithContext retrieves the value associated with a field in a hash with a context.
func (v *ValkeyCache) HashGetWithContext(ctx context.Context, key string, field string) ([]byte, error) {
	result, err := v.client.Do(ctx, v.client.B().Hget().Key(key).Field(field).Build()).AsBytes()
	if err != nil {
		return nil, translateValkeyError(err)
	}
	return result, nil
}

// HashGet retrieves the value associated with a field in a hash using context.Background().
func (v *ValkeyCache) HashGet(key string, field string) ([]byte, error) {
	return v.HashGetWithContext(context.Background(), key, field)
}

// HashGetAllWithContext retrieves all fields and values of a hash with a context.
func (v *ValkeyCache) HashGetAllWithContext(ctx context.Context, key string) (map[string][]byte, error) {
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

// HashGetAll retrieves all fields and values of a hash using context.Background().
func (v *ValkeyCache) HashGetAll(key string) (map[string][]byte, error) {
	return v.HashGetAllWithContext(context.Background(), key)
}

// HashDeleteWithContext deletes one or more hash fields with a context.
func (v *ValkeyCache) HashDeleteWithContext(ctx context.Context, key string, fields ...string) error {
	return v.client.Do(ctx, v.client.B().Hdel().Key(key).Field(fields...).Build()).Error()
}

// HashDelete deletes one or more hash fields using context.Background().
func (v *ValkeyCache) HashDelete(key string, fields ...string) error {
	return v.HashDeleteWithContext(context.Background(), key, fields...)
}

// ListPushWithContext inserts all the specified values at the tail of the list stored at key with a context.
func (v *ValkeyCache) ListPushWithContext(ctx context.Context, key string, values ...[]byte) error {
	args := make([]string, len(values))
	for i, v := range values {
		args[i] = string(v)
	}
	return v.client.Do(ctx, v.client.B().Rpush().Key(key).Element(args...).Build()).Error()
}

// ListPush inserts all the specified values at the tail of the list stored at key using context.Background().
func (v *ValkeyCache) ListPush(key string, values ...[]byte) error {
	return v.ListPushWithContext(context.Background(), key, values...)
}

// ListPopWithContext removes and returns the last element of the list stored at key with a context.
func (v *ValkeyCache) ListPopWithContext(ctx context.Context, key string) ([]byte, error) {
	result, err := v.client.Do(ctx, v.client.B().Lpop().Key(key).Build()).AsBytes()
	if err != nil {
		return nil, translateValkeyError(err)
	}
	return result, nil
}

// ListPop removes and returns the last element of the list stored at key using context.Background().
func (v *ValkeyCache) ListPop(key string) ([]byte, error) {
	return v.ListPopWithContext(context.Background(), key)
}

// ListRangeWithContext returns the specified elements of the list stored at key with a context.
func (v *ValkeyCache) ListRangeWithContext(ctx context.Context, key string, start, stop int64) ([][]byte, error) {
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

// ListRange returns the specified elements of the list stored at key using context.Background().
func (v *ValkeyCache) ListRange(key string, start, stop int64) ([][]byte, error) {
	return v.ListRangeWithContext(context.Background(), key, start, stop)
}

// SetAddWithContext adds the specified members to the set stored at key with a context.
func (v *ValkeyCache) SetAddWithContext(ctx context.Context, key string, members ...[]byte) error {
	args := make([]string, len(members))
	for i, m := range members {
		args[i] = string(m)
	}
	return v.client.Do(ctx, v.client.B().Sadd().Key(key).Member(args...).Build()).Error()
}

// SetAdd adds the specified members to the set stored at key using context.Background().
func (v *ValkeyCache) SetAdd(key string, members ...[]byte) error {
	return v.SetAddWithContext(context.Background(), key, members...)
}

// SetMembersWithContext returns all the members of the set stored at key with a context.
func (v *ValkeyCache) SetMembersWithContext(ctx context.Context, key string) ([][]byte, error) {
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

// SetMembers returns all the members of the set stored at key using context.Background().
func (v *ValkeyCache) SetMembers(key string) ([][]byte, error) {
	return v.SetMembersWithContext(context.Background(), key)
}

// SetIsMemberWithContext returns if member is a member of the set stored at key with a context.
func (v *ValkeyCache) SetIsMemberWithContext(ctx context.Context, key string, member []byte) (bool, error) {
	result, err := v.client.Do(ctx, v.client.B().Sismember().Key(key).Member(string(member)).Build()).AsInt64()
	if err != nil {
		return false, translateValkeyError(err)
	}
	return result == 1, nil
}

// SetIsMember returns if member is a member of the set stored at key using context.Background().
func (v *ValkeyCache) SetIsMember(key string, member []byte) (bool, error) {
	return v.SetIsMemberWithContext(context.Background(), key, member)
}

// Close closes the Valkey client.
func (v *ValkeyCache) Close() error {
	v.client.Close()
	return nil
}

// PingWithContext pings the Valkey server with a context.
func (v *ValkeyCache) PingWithContext(ctx context.Context) error {
	return v.client.Do(ctx, v.client.B().Ping().Build()).Error()
}

// Ping pings the Valkey server using context.Background().
func (v *ValkeyCache) Ping() error {
	return v.PingWithContext(context.Background())
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
	_ Cache            = (*ValkeyCache)(nil)
	_ CacheWithContext = (*ValkeyCache)(nil)
)

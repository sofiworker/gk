package gcache

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// GetOrLoad 命中则直接返回；未命中时调用 load 并把结果写回缓存。
// 读取遇到 ErrCacheMiss 以外的错误会直接上抛，避免后端故障被静默降级成穿透。
// 写回失败时同时返回已加载的值与错误，让调用方自行决定是否使用。
// GetOrLoad returns the cached value, or calls load and stores the result on a miss.
// A read error other than ErrCacheMiss propagates, so a broken backend is never silently
// degraded into unnoticed origin traffic. If the write-back fails, the loaded value is
// returned alongside the error so the caller can still use it.
func GetOrLoad(ctx context.Context, c KV, key string, ttl time.Duration, load func(context.Context) ([]byte, error)) ([]byte, error) {
	v, err := c.Get(ctx, key)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, ErrCacheMiss) {
		return nil, err
	}
	if load == nil {
		return nil, ErrNilLoader
	}

	loaded, err := load(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Set(ctx, key, loaded, ttl); err != nil {
		return loaded, err
	}
	return loaded, nil
}

// Exists 优先使用后端原生的存在性判断，否则回退到 Get。
// 这样「没有 EXISTS」的后端无需为了满足接口而写桩。
// Exists prefers the backend's native existence check and falls back to Get, so a backend
// without EXISTS never has to ship a stub just to satisfy an interface.
func Exists(ctx context.Context, c Getter, key string) (bool, error) {
	if e, ok := c.(Exister); ok {
		return e.Exists(ctx, key)
	}
	_, err := c.Get(ctx, key)
	if errors.Is(err, ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Incr 递增计数器并返回新值。
// Incr increments a counter and returns the new value.
func Incr(ctx context.Context, c Counter, key string, delta int64) (int64, error) {
	return c.Add(ctx, key, delta)
}

// Decr 递减计数器并返回新值。
// Decr decrements a counter and returns the new value.
func Decr(ctx context.Context, c Counter, key string, delta int64) (int64, error) {
	return c.Add(ctx, key, -delta)
}

// GetJSON 读取并反序列化为 T；未命中时返回 T 的零值与 ErrCacheMiss。
// GetJSON reads a key and unmarshals it into T; on a miss it returns T's zero value
// along with ErrCacheMiss.
func GetJSON[T any](ctx context.Context, c Getter, key string) (T, error) {
	var v T
	b, err := c.Get(ctx, key)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// SetJSON 把 value 序列化为 JSON 后写入。
// SetJSON marshals value as JSON and stores it.
func SetJSON[T any](ctx context.Context, c Setter, key string, value T, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, b, ttl)
}

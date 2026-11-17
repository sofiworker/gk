package gcache

import (
	"context"
	"time"
)

// Funcs 用闭包实现 KV，适合测试替身或一次性包装，省去为三个方法专门定义类型。
// 字段为 nil 时对应方法返回 ErrNotImplemented。
// Funcs implements KV via closures, which suits test doubles and one-off wrappers without
// declaring a type for three methods. A nil field makes the corresponding method return
// ErrNotImplemented.
//
// 它刻意只覆盖 KV：若在此追加 Exists/TTL 等可选能力字段，Funcs 就会无条件满足那些接口，
// 使「用类型断言探测能力」失效 —— 字段为 nil 的能力在断言时依旧显示为可用。
// 需要可选能力的注入方请定义自己的类型，只实现真正支持的接口。
// It deliberately covers KV only: adding optional-capability fields such as Exists or TTL
// would make Funcs satisfy those interfaces unconditionally and break capability detection
// by type assertion, since a nil field would still assert as available. Injectors needing
// optional capabilities should declare their own type and implement only what they support.
type Funcs struct {
	GetFunc    func(ctx context.Context, key string) ([]byte, error)
	SetFunc    func(ctx context.Context, key string, value []byte, ttl time.Duration) error
	DeleteFunc func(ctx context.Context, key string) error
}

func (f Funcs) Get(ctx context.Context, key string) ([]byte, error) {
	if f.GetFunc == nil {
		return nil, ErrNotImplemented
	}
	return f.GetFunc(ctx, key)
}

func (f Funcs) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if f.SetFunc == nil {
		return ErrNotImplemented
	}
	return f.SetFunc(ctx, key, value, ttl)
}

func (f Funcs) Delete(ctx context.Context, key string) error {
	if f.DeleteFunc == nil {
		return ErrNotImplemented
	}
	return f.DeleteFunc(ctx, key)
}

var _ KV = Funcs{}

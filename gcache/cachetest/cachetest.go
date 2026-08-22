// Package cachetest 提供按能力拆分的 gcache 契约一致性套件。
// 每个 Run* 只验证一种能力，实现支持哪些就跑哪些，因此无需按具体类型开洞。
// 注入自建后端的用户可以 import 本包自证实现符合契约。
// Package cachetest provides gcache conformance suites split by capability. Each Run* checks
// one capability, so an implementation runs exactly the suites it supports and no suite needs
// to special-case a concrete type. Users injecting their own backend can import this package
// to prove their implementation honours the contracts.
//
// 本包只依赖标准库 testing，不破坏 gcache 的零第三方依赖约束。
// It depends only on the standard library's testing package, preserving gcache's
// zero-third-party-dependency constraint.
package cachetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sofiworker/gk/gcache"
)

// RunKV 验证 gcache.KV 契约：读写、覆盖、未命中、删除幂等与返回值所有权。
// RunKV verifies the gcache.KV contract: read/write, overwrite, miss, idempotent delete
// and ownership of returned values.
func RunKV(t *testing.T, c gcache.KV) {
	t.Helper()
	ctx := context.Background()

	t.Run("SetGet", func(t *testing.T) {
		key := prepareKey(t, c, "basic")
		if err := c.Set(ctx, key, []byte("v1"), 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := c.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != "v1" {
			t.Fatalf("Get = %q, want %q", got, "v1")
		}
	})

	t.Run("Overwrite", func(t *testing.T) {
		key := prepareKey(t, c, "overwrite")
		if err := c.Set(ctx, key, []byte("first"), 0); err != nil {
			t.Fatalf("first Set: %v", err)
		}
		if err := c.Set(ctx, key, []byte("second"), 0); err != nil {
			t.Fatalf("second Set: %v", err)
		}
		got, err := c.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != "second" {
			t.Fatalf("Get = %q, want %q", got, "second")
		}
	})

	t.Run("Miss", func(t *testing.T) {
		key := prepareKey(t, c, "absent")
		if _, err := c.Get(ctx, key); !errors.Is(err, gcache.ErrCacheMiss) {
			t.Fatalf("Get on absent key = %v, want ErrCacheMiss", err)
		}
	})

	t.Run("EmptyValue", func(t *testing.T) {
		key := prepareKey(t, c, "empty")
		if err := c.Set(ctx, key, []byte{}, 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := c.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get on empty value = %v, want a hit", err)
		}
		if len(got) != 0 {
			t.Fatalf("Get = %q, want empty", got)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		key := prepareKey(t, c, "delete")
		if err := c.Set(ctx, key, []byte("v"), 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := c.Delete(ctx, key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := c.Get(ctx, key); !errors.Is(err, gcache.ErrCacheMiss) {
			t.Fatalf("Get after Delete = %v, want ErrCacheMiss", err)
		}
	})

	t.Run("DeleteAbsentIsIdempotent", func(t *testing.T) {
		key := prepareKey(t, c, "delete-absent")
		if err := c.Delete(ctx, key); err != nil {
			t.Fatalf("Delete on absent key = %v, want nil", err)
		}
	})

	t.Run("ReturnedValueIsCallerOwned", func(t *testing.T) {
		key := prepareKey(t, c, "ownership")
		if err := c.Set(ctx, key, []byte("v1"), 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := c.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		got[0] = 'X'

		again, err := c.Get(ctx, key)
		if err != nil {
			t.Fatalf("second Get: %v", err)
		}
		if !bytes.Equal(again, []byte("v1")) {
			t.Fatalf("mutating a returned slice corrupted the cache: got %q, want %q", again, "v1")
		}
	})
}

// RunTTL 验证写入时 TTL，以及可选的 gcache.TTLReader 与 gcache.Expirer；
// 实现未提供对应能力时自动跳过。
// RunTTL verifies write-time TTL plus the optional gcache.TTLReader and gcache.Expirer,
// skipping whichever capability the implementation does not provide.
func RunTTL(t *testing.T, c gcache.KV) {
	t.Helper()
	ctx := context.Background()

	t.Run("ExpiresAfterTTL", func(t *testing.T) {
		key := prepareKey(t, c, "expires")
		if err := c.Set(ctx, key, []byte("v"), 100*time.Millisecond); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if _, err := c.Get(ctx, key); err != nil {
			t.Fatalf("Get before expiry: %v", err)
		}

		time.Sleep(250 * time.Millisecond)
		if _, err := c.Get(ctx, key); !errors.Is(err, gcache.ErrCacheMiss) {
			t.Fatalf("Get after expiry = %v, want ErrCacheMiss", err)
		}
	})

	t.Run("TTLReader", func(t *testing.T) {
		reader, ok := c.(gcache.TTLReader)
		if !ok {
			t.Skip("implementation does not provide gcache.TTLReader")
		}

		key := prepareKey(t, c, "ttl")
		if err := c.Set(ctx, key, []byte("v"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		ttl, err := reader.TTL(ctx, key)
		if err != nil {
			t.Fatalf("TTL: %v", err)
		}
		if ttl <= 0 || ttl > time.Minute {
			t.Fatalf("TTL = %v, want a positive value not exceeding %v", ttl, time.Minute)
		}

		persistent := prepareKey(t, c, "persistent")
		if err := c.Set(ctx, persistent, []byte("v"), 0); err != nil {
			t.Fatalf("Set without ttl: %v", err)
		}
		ttl, err = reader.TTL(ctx, persistent)
		if err != nil {
			t.Fatalf("TTL without expiration: %v", err)
		}
		if ttl >= 0 {
			t.Fatalf("TTL without expiration = %v, want a negative value", ttl)
		}

		absent := prepareKey(t, c, "ttl-absent")
		if _, err := reader.TTL(ctx, absent); !errors.Is(err, gcache.ErrCacheMiss) {
			t.Fatalf("TTL on absent key = %v, want ErrCacheMiss", err)
		}
	})

	t.Run("Expirer", func(t *testing.T) {
		expirer, ok := c.(gcache.Expirer)
		if !ok {
			t.Skip("implementation does not provide gcache.Expirer")
		}

		key := prepareKey(t, c, "expire")
		if err := c.Set(ctx, key, []byte("v"), 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := expirer.Expire(ctx, key, 100*time.Millisecond); err != nil {
			t.Fatalf("Expire: %v", err)
		}
		time.Sleep(250 * time.Millisecond)
		if _, err := c.Get(ctx, key); !errors.Is(err, gcache.ErrCacheMiss) {
			t.Fatalf("Get after Expire = %v, want ErrCacheMiss", err)
		}

		absent := prepareKey(t, c, "expire-absent")
		if err := expirer.Expire(ctx, absent, time.Minute); !errors.Is(err, gcache.ErrCacheMiss) {
			t.Fatalf("Expire on absent key = %v, want ErrCacheMiss", err)
		}
	})
}

// RunCounter 验证 gcache.Counter 契约：从零起算、按 delta 增减。
// RunCounter verifies the gcache.Counter contract: counters start at zero and move by delta.
func RunCounter(t *testing.T, c gcache.Counter) {
	t.Helper()
	ctx := context.Background()
	deleter, _ := c.(gcache.Deleter)

	t.Run("StartsFromZero", func(t *testing.T) {
		key := prepareKey(t, deleter, "fresh")
		got, err := c.Add(ctx, key, 7)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if got != 7 {
			t.Fatalf("Add(7) on a fresh key = %d, want 7", got)
		}
	})

	t.Run("AddAndSubtract", func(t *testing.T) {
		key := prepareKey(t, deleter, "counter")
		steps := []struct {
			delta int64
			want  int64
		}{
			{delta: 1, want: 1},
			{delta: 4, want: 5},
			{delta: -2, want: 3},
		}
		for _, step := range steps {
			got, err := c.Add(ctx, key, step.delta)
			if err != nil {
				t.Fatalf("Add(%d): %v", step.delta, err)
			}
			if got != step.want {
				t.Fatalf("Add(%d) = %d, want %d", step.delta, got, step.want)
			}
		}
	})
}

// prepareKey 返回本次断言专用的键。d 为 nil 表示实现无法删除，
// 此时退化为带时间戳的一次性键，避免上一次运行的残留影响断言。
// prepareKey returns a key reserved for this assertion. A nil d means the implementation
// cannot delete, so the key falls back to a timestamped one-shot name and residue from a
// previous run cannot affect the assertion.
func prepareKey(t *testing.T, d gcache.Deleter, name string) string {
	t.Helper()
	base := "gcache-conformance:" + t.Name() + ":" + name
	if d == nil {
		return fmt.Sprintf("%s:%d", base, time.Now().UnixNano())
	}
	if err := d.Delete(context.Background(), base); err != nil {
		t.Fatalf("Delete(%q) while preparing the key: %v", base, err)
	}
	t.Cleanup(func() { _ = d.Delete(context.Background(), base) })
	return base
}

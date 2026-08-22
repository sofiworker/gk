package gcache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFuncsWithoutFieldsReportsNotImplemented(t *testing.T) {
	var empty Funcs
	ctx := context.Background()

	if _, err := empty.Get(ctx, "key"); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Get = %v, want ErrNotImplemented", err)
	}
	if err := empty.Set(ctx, "key", []byte("v"), 0); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Set = %v, want ErrNotImplemented", err)
	}
	if err := empty.Delete(ctx, "key"); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Delete = %v, want ErrNotImplemented", err)
	}
}

func TestFuncsForwardsArguments(t *testing.T) {
	var (
		gotGetKey    string
		gotSetKey    string
		gotSetValue  []byte
		gotSetTTL    time.Duration
		gotDeleteKey string
	)

	f := Funcs{
		GetFunc: func(_ context.Context, key string) ([]byte, error) {
			gotGetKey = key
			return []byte("v"), nil
		},
		SetFunc: func(_ context.Context, key string, value []byte, ttl time.Duration) error {
			gotSetKey, gotSetValue, gotSetTTL = key, value, ttl
			return nil
		},
		DeleteFunc: func(_ context.Context, key string) error {
			gotDeleteKey = key
			return nil
		},
	}

	ctx := context.Background()
	if _, err := f.Get(ctx, "get-key"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := f.Set(ctx, "set-key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := f.Delete(ctx, "delete-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if gotGetKey != "get-key" {
		t.Errorf("GetFunc key = %q, want %q", gotGetKey, "get-key")
	}
	if gotSetKey != "set-key" || string(gotSetValue) != "value" || gotSetTTL != time.Minute {
		t.Errorf("SetFunc got (%q, %q, %v), want (%q, %q, %v)",
			gotSetKey, gotSetValue, gotSetTTL, "set-key", "value", time.Minute)
	}
	if gotDeleteKey != "delete-key" {
		t.Errorf("DeleteFunc key = %q, want %q", gotDeleteKey, "delete-key")
	}
}

// Funcs 只覆盖 KV：追加可选能力字段会让它无条件满足那些接口，破坏类型断言式能力探测。
func TestFuncsExposesNoOptionalCapabilities(t *testing.T) {
	var injected any = Funcs{}

	if _, ok := injected.(Exister); ok {
		t.Error("Funcs must not satisfy Exister")
	}
	if _, ok := injected.(TTLReader); ok {
		t.Error("Funcs must not satisfy TTLReader")
	}
	if _, ok := injected.(Expirer); ok {
		t.Error("Funcs must not satisfy Expirer")
	}
	if _, ok := injected.(Counter); ok {
		t.Error("Funcs must not satisfy Counter")
	}
}

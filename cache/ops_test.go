package gcache

import (
	"context"
	"errors"
	"testing"
	"time"
)

// existerStub 借 Funcs 拿到 KV 方法，再补一个原生 Exists，用于验证能力探测走了原生路径。
type existerStub struct {
	Funcs
	existsCalls int
}

func (e *existerStub) Exists(context.Context, string) (bool, error) {
	e.existsCalls++
	return true, nil
}

func TestGetOrLoadReturnsCachedValueWithoutLoading(t *testing.T) {
	cache := newTestMemoryCache(t)
	ctx := context.Background()

	loads := 0
	load := func(context.Context) ([]byte, error) {
		loads++
		return []byte("loaded"), nil
	}

	v, err := GetOrLoad(ctx, cache, "key", time.Minute, load)
	if err != nil || string(v) != "loaded" || loads != 1 {
		t.Fatalf("first GetOrLoad = %q, %v (loads=%d)", v, err, loads)
	}

	v, err = GetOrLoad(ctx, cache, "key", time.Minute, load)
	if err != nil || string(v) != "loaded" || loads != 1 {
		t.Fatalf("second GetOrLoad = %q, %v (loads=%d), want the cached value", v, err, loads)
	}
}

func TestGetOrLoadPropagatesLoaderError(t *testing.T) {
	cache := newTestMemoryCache(t)
	wantErr := errors.New("load failed")

	_, err := GetOrLoad(context.Background(), cache, "key", time.Minute, func(context.Context) ([]byte, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("GetOrLoad = %v, want the loader error", err)
	}
}

func TestGetOrLoadWithoutLoader(t *testing.T) {
	cache := newTestMemoryCache(t)

	if _, err := GetOrLoad(context.Background(), cache, "key", time.Minute, nil); !errors.Is(err, ErrNilLoader) {
		t.Fatalf("GetOrLoad = %v, want ErrNilLoader", err)
	}
}

func TestGetOrLoadPropagatesNonMissReadError(t *testing.T) {
	readErr := errors.New("backend unavailable")
	cache := Funcs{
		GetFunc: func(context.Context, string) ([]byte, error) { return nil, readErr },
	}

	loads := 0
	_, err := GetOrLoad(context.Background(), cache, "key", time.Minute, func(context.Context) ([]byte, error) {
		loads++
		return []byte("loaded"), nil
	})
	if !errors.Is(err, readErr) {
		t.Fatalf("GetOrLoad = %v, want the read error", err)
	}
	if loads != 0 {
		t.Fatalf("loader ran %d times, want 0: a broken backend must not be silently degraded", loads)
	}
}

func TestGetOrLoadReturnsLoadedValueWhenWriteBackFails(t *testing.T) {
	writeErr := errors.New("write failed")
	cache := Funcs{
		GetFunc: func(context.Context, string) ([]byte, error) { return nil, ErrCacheMiss },
		SetFunc: func(context.Context, string, []byte, time.Duration) error { return writeErr },
	}

	v, err := GetOrLoad(context.Background(), cache, "key", time.Minute, func(context.Context) ([]byte, error) {
		return []byte("loaded"), nil
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("GetOrLoad error = %v, want the write error", err)
	}
	if string(v) != "loaded" {
		t.Fatalf("GetOrLoad value = %q, want the loaded value to survive a failed write-back", v)
	}
}

func TestExistsPrefersNativeExister(t *testing.T) {
	// GetFunc 为 nil：一旦回退到 Get 就会拿到 ErrNotImplemented，从而暴露走错路径。
	stub := &existerStub{}

	ok, err := Exists(context.Background(), stub, "key")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatal("Exists = false, want true")
	}
	if stub.existsCalls != 1 {
		t.Fatalf("native Exists called %d times, want 1", stub.existsCalls)
	}
}

func TestExistsFallsBackToGet(t *testing.T) {
	cache := newTestMemoryCache(t)
	ctx := context.Background()
	if err := cache.Set(ctx, "key", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Funcs 不实现 Exister，因此必然走 Get 回退路径。
	getOnly := Funcs{GetFunc: cache.Get}

	ok, err := Exists(ctx, getOnly, "key")
	if err != nil {
		t.Fatalf("Exists on present key: %v", err)
	}
	if !ok {
		t.Fatal("Exists on present key = false, want true")
	}

	ok, err = Exists(ctx, getOnly, "absent")
	if err != nil {
		t.Fatalf("Exists on absent key: %v", err)
	}
	if ok {
		t.Fatal("Exists on absent key = true, want false")
	}
}

func TestExistsPropagatesReadError(t *testing.T) {
	readErr := errors.New("backend unavailable")
	getOnly := Funcs{
		GetFunc: func(context.Context, string) ([]byte, error) { return nil, readErr },
	}

	if _, err := Exists(context.Background(), getOnly, "key"); !errors.Is(err, readErr) {
		t.Fatalf("Exists = %v, want the read error", err)
	}
}

func TestIncrAndDecr(t *testing.T) {
	cache := newTestMemoryCache(t)
	ctx := context.Background()

	if got, err := Incr(ctx, cache, "counter", 5); err != nil || got != 5 {
		t.Fatalf("Incr = %d, %v, want 5, nil", got, err)
	}
	if got, err := Decr(ctx, cache, "counter", 2); err != nil || got != 3 {
		t.Fatalf("Decr = %d, %v, want 3, nil", got, err)
	}
}

func TestGetJSONAndSetJSON(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	cache := newTestMemoryCache(t)
	ctx := context.Background()
	want := payload{Name: "gk", Count: 3}

	if err := SetJSON(ctx, cache, "key", want, time.Minute); err != nil {
		t.Fatalf("SetJSON: %v", err)
	}
	got, err := GetJSON[payload](ctx, cache, "key")
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got != want {
		t.Fatalf("GetJSON = %+v, want %+v", got, want)
	}
}

func TestGetJSONOnMissReturnsZeroValue(t *testing.T) {
	cache := newTestMemoryCache(t)

	got, err := GetJSON[int](context.Background(), cache, "absent")
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("GetJSON = %v, want ErrCacheMiss", err)
	}
	if got != 0 {
		t.Fatalf("GetJSON value = %d, want the zero value", got)
	}
}

func TestGetJSONOnMalformedPayload(t *testing.T) {
	cache := newTestMemoryCache(t)
	ctx := context.Background()
	if err := cache.Set(ctx, "key", []byte("{not json"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, err := GetJSON[map[string]string](ctx, cache, "key"); err == nil {
		t.Fatal("GetJSON on a malformed payload should fail")
	}
}

func TestSetJSONRejectsUnmarshalableValue(t *testing.T) {
	cache := newTestMemoryCache(t)

	if err := SetJSON(context.Background(), cache, "key", make(chan int), time.Minute); err == nil {
		t.Fatal("SetJSON on an unmarshalable value should fail")
	}
}

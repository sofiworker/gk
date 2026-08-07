package gsd

import (
	"errors"
	"testing"
	"time"

	"github.com/sofiworker/gk/gretry"
)

func TestRetryWithBackoff(t *testing.T) {
	opts := DefaultErrorHandlingOptions
	opts.MaxRetries = 2
	opts.RetryDelay = 1 * time.Millisecond
	opts.RetryStrategy = RetryStrategyFixed
	opts.ShouldRetry = func(err error) bool { return true }

	count := 0
	fn := func() error {
		count++
		return errors.New("fail")
	}

	err := retryWithBackoff(fn, opts)
	if err == nil {
		t.Error("expected error")
	}
	if count != 3 { // 1 次初始 + 2 次重试；1 initial + 2 retries.
		t.Errorf("expected 3 attempts, got %d", count)
	}

	// 成功场景；successful case.
	count = 0
	fnSuccess := func() error {
		count++
		if count < 2 {
			return errors.New("fail")
		}
		return nil
	}
	err = retryWithBackoff(fnSuccess, opts)
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 attempts, got %d", count)
	}
}

func TestCalculateDelay(t *testing.T) {
	opts := DefaultErrorHandlingOptions
	opts.RetryDelay = 10 * time.Millisecond
	opts.BackoffMultiplier = 2.0
	opts.MaxRetryDelay = 100 * time.Millisecond

	gopts := gretry.ErrorHandlingOptions{
		RetryDelay:        opts.RetryDelay,
		BackoffMultiplier: opts.BackoffMultiplier,
		MaxRetryDelay:     opts.MaxRetryDelay,
	}

	opts.RetryStrategy = RetryStrategyFixed
	gopts.RetryStrategy = gretry.RetryStrategyFixed
	d := gretry.NextDelay(0, gopts)
	if d < 10*time.Millisecond {
		t.Errorf("expected >= 10ms, got %v", d)
	}

	opts.RetryStrategy = RetryStrategyLinear
	gopts.RetryStrategy = gretry.RetryStrategyLinear
	d = gretry.NextDelay(1, gopts) // 10 * 2 = 20ms
	if d < 20*time.Millisecond {
		t.Errorf("expected >= 20ms, got %v", d)
	}

	opts.RetryStrategy = RetryStrategyExponential
	gopts.RetryStrategy = gretry.RetryStrategyExponential
	d = gretry.NextDelay(1, gopts) // 10 * 2^1 = 20ms
	if d < 20*time.Millisecond {
		t.Errorf("expected >= 20ms, got %v", d)
	}
}

func TestLoadBalancers(t *testing.T) {
	services := []ServiceInfo{
		{Name: "s1", Address: "a1"},
		{Name: "s2", Address: "a2"},
	}

	rlb := NewRandomLoadBalancer()
	if s := rlb.Select(nil); s != nil {
		t.Error("expected nil for empty services")
	}
	s := rlb.Select(services)
	if s == nil {
		t.Error("expected service")
	}

	rrlb := NewRoundRobinLoadBalancer()
	if s := rrlb.Select(nil); s != nil {
		t.Error("expected nil for empty services")
	}

	s1 := rrlb.Select(services)
	s2 := rrlb.Select(services)
	s3 := rrlb.Select(services)

	if s1.Address != "a1" {
		t.Error("expected a1")
	}
	if s2.Address != "a2" {
		t.Error("expected a2")
	}
	if s3.Address != "a1" {
		t.Error("expected a1")
	}
}

func TestKeyFormatter(t *testing.T) {
	kf := NewDefaultKeyFormatter("/root")
	si := ServiceInfo{Name: "foo", Address: "1.1.1.1", Port: 80}
	key := kf.Format(si)
	if key != "/root/foo/1.1.1.1:80" {
		t.Errorf("Format failed: %s", key)
	}

	// 默认解析未完整实现，仅返回空值；default parsing is not fully implemented and returns empty.
	_, _ = kf.Parse(key)
}

func TestCustomService(t *testing.T) {
	cs := CustomService{
		Name:    "custom",
		Address: "addr",
		Port:    8080,
		Version: "v1",
		Weight:  10,
		Status:  ServiceStatusHealthy,
	}

	if cs.GetName() != "custom" {
		t.Error("GetName failed")
	}
	meta := cs.GetMetadata()
	if meta["version"] != "v1" {
		t.Error("metadata failed")
	}

	si := cs.ToServiceInfo()
	if si.Name != "custom" {
		t.Error("ToServiceInfo failed")
	}
}

func TestBuildErrorHandlingOptions(t *testing.T) {
	opts := BuildErrorHandlingOptions(
		WithMaxRetries(5),
		WithRetryDelay(time.Second),
	)
	if opts.MaxRetries != 5 {
		t.Error("WithMaxRetries failed")
	}
	if opts.RetryDelay != time.Second {
		t.Error("WithRetryDelay failed")
	}
}

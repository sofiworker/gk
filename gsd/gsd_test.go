package gsd

import (
	"errors"
	"testing"
	"time"
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
	if count != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 attempts, got %d", count)
	}

	// Test success
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

	// Fixed
	opts.RetryStrategy = RetryStrategyFixed
	d := calculateDelay(0, opts)
	if d < 10*time.Millisecond {
		t.Errorf("expected >= 10ms, got %v", d)
	}

	// Linear
	opts.RetryStrategy = RetryStrategyLinear
	d = calculateDelay(1, opts) // 10 * 2 = 20ms
	if d < 20*time.Millisecond {
		t.Errorf("expected >= 20ms, got %v", d)
	}

	// Exponential
	opts.RetryStrategy = RetryStrategyExponential
	d = calculateDelay(1, opts) // 10 * 2^1 = 20ms
	if d < 20*time.Millisecond {
		t.Errorf("expected >= 20ms, got %v", d)
	}
}

func TestLoadBalancers(t *testing.T) {
	services := []ServiceInfo{
		{Name: "s1", Address: "a1"},
		{Name: "s2", Address: "a2"},
	}

	// Random
	rlb := NewRandomLoadBalancer()
	if s := rlb.Select(nil); s != nil {
		t.Error("expected nil for empty services")
	}
	s := rlb.Select(services)
	if s == nil {
		t.Error("expected service")
	}

	// RoundRobin
	rrlb := NewRoundRobinLoadBalancer()
	if s := rrlb.Select(nil); s != nil {
		t.Error("expected nil for empty services")
	}
	
	s1 := rrlb.Select(services)
	s2 := rrlb.Select(services)
	s3 := rrlb.Select(services)
	
	if s1.Address != "a1" { t.Error("expected a1") }
	if s2.Address != "a2" { t.Error("expected a2") }
	if s3.Address != "a1" { t.Error("expected a1") }
}

func TestKeyFormatter(t *testing.T) {
	kf := NewDefaultKeyFormatter("/root")
	si := ServiceInfo{Name: "foo", Address: "1.1.1.1", Port: 80}
	key := kf.Format(si)
	if key != "/root/foo/1.1.1.1:80" {
		t.Errorf("Format failed: %s", key)
	}
	
	// Parse is not implemented fully in default, just returns empty
	_, _ = kf.Parse(key)
}

func TestCustomService(t *testing.T) {
	cs := CustomService{
		Name: "custom",
		Address: "addr",
		Port: 8080,
		Version: "v1",
		Weight: 10,
		Status: ServiceStatusHealthy,
	}
	
	if cs.GetName() != "custom" { t.Error("GetName failed") }
	meta := cs.GetMetadata()
	if meta["version"] != "v1" { t.Error("metadata failed") }
	
	si := cs.ToServiceInfo()
	if si.Name != "custom" { t.Error("ToServiceInfo failed") }
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

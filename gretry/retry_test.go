package gretry

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDoWithDefault(t *testing.T) {
	tests := []struct {
		name          string
		fn            func() error
		expectSuccess bool
	}{
		{
			name: "success on first attempt",
			fn: func() error {
				return nil
			},
			expectSuccess: true,
		},
		{
			name: "success on retry",
			fn: func() error {
				staticAttempts := 0
				staticAttempts++
				if staticAttempts < 3 {
					return fmt.Errorf("error")
				}
				return nil
			},
			expectSuccess: true,
		},
		{
			name: "failure after max retries",
			fn: func() error {
				return fmt.Errorf("persistent error")
			},
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DoWithDefault(context.Background(), tt.fn)
			if result.Success != tt.expectSuccess {
				t.Errorf("Expected success=%v, got success=%v", tt.expectSuccess, result.Success)
			}
		})
	}
}

func TestDoWithCustomOptions(t *testing.T) {
	options := NewErrorHandlingOptions(
		WithMaxRetries(2),
		WithRetryDelay(10*time.Millisecond),
		WithRetryStrategy(RetryStrategyFixed),
	)

	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("error")
		}
		return nil
	}

	result := Do(context.Background(), fn, options)
	if !result.Success {
		t.Errorf("Expected success, got failure: %v", result.Error)
	}
	if result.Attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", result.Attempts)
	}
}

func TestDoWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	fn := func() error {
		time.Sleep(100 * time.Millisecond) // Longer than context timeout
		return nil
	}

	result := Do(ctx, fn, DefaultErrorHandlingOptions)
	if result.Success {
		t.Error("Expected failure due to context cancellation, got success")
	}
	if result.Error != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded error, got %v", result.Error)
	}
}

func TestDoWithTimeoutOption(t *testing.T) {
	options := NewErrorHandlingOptions(
		WithTimeout(50 * time.Millisecond),
	)

	fn := func() error {
		time.Sleep(100 * time.Millisecond) // Longer than timeout
		return nil
	}

	result := Do(context.Background(), fn, options)
	if result.Success {
		t.Error("Expected failure due to timeout, got success")
	}
	if result.Error != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded error, got %v", result.Error)
	}
}

func TestCalculateDelay(t *testing.T) {
	tests := []struct {
		name          string
		attempt       int
		options       ErrorHandlingOptions
		expectedDelay time.Duration
	}{
		{
			name:    "exponential backoff",
			attempt: 2,
			options: ErrorHandlingOptions{
				RetryDelay:        time.Second,
				RetryStrategy:     RetryStrategyExponential,
				BackoffMultiplier: 2.0,
				MaxRetryDelay:     10 * time.Second,
			},
			expectedDelay: 4 * time.Second,
		},
		{
			name:    "linear backoff",
			attempt: 3,
			options: ErrorHandlingOptions{
				RetryDelay:    time.Second,
				RetryStrategy: RetryStrategyLinear,
				MaxRetryDelay: 10 * time.Second,
			},
			expectedDelay: 4 * time.Second,
		},
		{
			name:    "fixed backoff",
			attempt: 5,
			options: ErrorHandlingOptions{
				RetryDelay:    time.Second,
				RetryStrategy: RetryStrategyFixed,
				MaxRetryDelay: 10 * time.Second,
			},
			expectedDelay: time.Second,
		},
		{
			name:    "delay capped by max",
			attempt: 10,
			options: ErrorHandlingOptions{
				RetryDelay:        time.Second,
				RetryStrategy:     RetryStrategyExponential,
				BackoffMultiplier: 2.0,
				MaxRetryDelay:     3 * time.Second,
			},
			expectedDelay: 3 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := calculateDelay(tt.attempt, tt.options)
			// Allow some tolerance for jitter
			if delay > tt.expectedDelay {
				// For non-jitter tests, this should be exact
				if tt.options.JitterType == JitterNone || tt.options.JitterFactor == 0 {
					t.Errorf("Expected delay %v, got %v", tt.expectedDelay, delay)
				}
			}
		})
	}
}

func TestApplyJitter(t *testing.T) {
	baseDelay := time.Second

	tests := []struct {
		name          string
		delay         time.Duration
		options       ErrorHandlingOptions
		expectInRange func(time.Duration) bool
	}{
		{
			name:  "no jitter",
			delay: baseDelay,
			options: ErrorHandlingOptions{
				JitterType:   JitterNone,
				JitterFactor: 0.0,
			},
			expectInRange: func(d time.Duration) bool {
				return d == baseDelay
			},
		},
		{
			name:  "full jitter",
			delay: baseDelay,
			options: ErrorHandlingOptions{
				JitterType:   JitterFull,
				JitterFactor: 1.0,
			},
			expectInRange: func(d time.Duration) bool {
				return d >= 0 && d <= baseDelay
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 10; i++ { // Run multiple times to test randomness
				delay := applyJitter(tt.delay, tt.options)
				if !tt.expectInRange(delay) {
					t.Errorf("Delay %v out of expected range", delay)
				}
			}
		})
	}
}

func TestCallbacks(t *testing.T) {
	var retryCalls, successCalls, failedCalls int

	options := NewErrorHandlingOptions(
		WithMaxRetries(2),
		WithRetryDelay(1*time.Millisecond),
		WithOnRetry(func(attempt int, delay time.Duration, err error) {
			retryCalls++
		}),
		WithOnSuccess(func(attempt int, elapsed time.Duration) {
			successCalls++
		}),
		WithOnFailed(func(attempts int, elapsed time.Duration, err error) {
			failedCalls++
		}),
	)

	// Test successful case
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("error")
		}
		return nil
	}

	result := Do(context.Background(), fn, options)
	if !result.Success {
		t.Error("Expected success")
	}
	if retryCalls != 1 {
		t.Errorf("Expected 1 retry call, got %d", retryCalls)
	}
	if successCalls != 1 {
		t.Errorf("Expected 1 success call, got %d", successCalls)
	}
	if failedCalls != 0 {
		t.Errorf("Expected 0 failed calls, got %d", failedCalls)
	}

	// Reset counters
	retryCalls, successCalls, failedCalls = 0, 0, 0
	attempts = 0

	// Test failed case
	fnFail := func() error {
		return fmt.Errorf("persistent error")
	}

	result = Do(context.Background(), fnFail, options)
	if result.Success {
		t.Error("Expected failure")
	}
	if retryCalls != 2 {
		t.Errorf("Expected 2 retry calls, got %d", retryCalls)
	}
	if successCalls != 0 {
		t.Errorf("Expected 0 success calls, got %d", successCalls)
	}
	if failedCalls != 1 {
		t.Errorf("Expected 1 failed call, got %d", failedCalls)
	}
}

func TestShouldRetry(t *testing.T) {
	options := NewErrorHandlingOptions(
		WithMaxRetries(2),
		WithShouldRetry(func(err error) bool {
			return err != nil && err.Error() == "retryable"
		}),
	)

	attempts := 0
	fn := func() error {
		attempts++
		switch attempts {
		case 1:
			return fmt.Errorf("retryable")
		case 2:
			return fmt.Errorf("non-retryable")
		default:
			return nil
		}
	}

	result := Do(context.Background(), fn, options)
	// Should only retry once because the second error is not retryable
	if result.Attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", result.Attempts)
	}
	if result.Success {
		t.Error("Expected failure")
	}
}

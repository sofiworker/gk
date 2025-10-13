package gclient

import (
	"time"

	"golang.org/x/exp/rand"
)

type RetryConfig struct {
	MaxRetries      int
	RetryConditions []RetryCondition
	Backoff         BackoffStrategy
	MaxRetryTime    time.Duration
}

type RetryCondition func(*Response, error) bool

type BackoffStrategy func(attempt int) time.Duration

func ExponentialBackoff(baseDelay time.Duration) BackoffStrategy {
	return func(attempt int) time.Duration {
		delay := baseDelay * time.Duration(1<<uint(attempt))
		jitter := time.Duration(rand.Int63n(int64(delay) / 2))
		return delay + jitter
	}
}

func DefaultRetryCondition(resp *Response, err error) bool {
	return true
}

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:      3,
		RetryConditions: []RetryCondition{DefaultRetryCondition},
		Backoff:         ExponentialBackoff(500 * time.Millisecond),
		MaxRetryTime:    5 * time.Second,
	}
}

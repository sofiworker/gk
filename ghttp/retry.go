package ghttp

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

	if err != nil {
		return true
	}

	// 5xx 服务器错误重试
	if resp.fResp.StatusCode() >= 500 {
		return true
	}

	// 429 限流重试
	if resp.fResp.StatusCode() == 429 {
		return true
	}

	return false
}

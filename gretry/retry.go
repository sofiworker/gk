package gretry

import (
	"fmt"
	"log"
	"math/rand"
	"time"
)

// ErrorHandlingOptions 错误处理选项
type ErrorHandlingOptions struct {
	MaxRetries          int              `json:"max_retries"`
	RetryDelay          time.Duration    `json:"retry_delay"`
	MaxRetryDelay       time.Duration    `json:"max_retry_delay"`
	RetryStrategy       RetryStrategy    `json:"retry_strategy"`
	BackoffMultiplier   float64          `json:"backoff_multiplier"`
	TransientErrors     []string         `json:"transient_errors"`
	ShouldRetry         func(error) bool `json:"-"`
	Timeout             time.Duration    `json:"timeout"`
	HealthCheckInterval time.Duration    `json:"health_check_interval"`
}

// DefaultErrorHandlingOptions 默认错误处理选项
var DefaultErrorHandlingOptions = ErrorHandlingOptions{
	MaxRetries:          3,
	RetryDelay:          1 * time.Second,
	MaxRetryDelay:       30 * time.Second,
	RetryStrategy:       RetryStrategyExponential,
	BackoffMultiplier:   2.0,
	TransientErrors:     []string{"connection refused", "timeout", "unavailable", "etcdserver: leader changed"},
	Timeout:             10 * time.Second,
	HealthCheckInterval: 30 * time.Second,
	ShouldRetry: func(err error) bool {
		if err == nil {
			return false
		}
		return false
	},
}

// RetryStrategy 重试策略
type RetryStrategy string

const (
	RetryStrategyExponential RetryStrategy = "exponential"
	RetryStrategyLinear      RetryStrategy = "linear"
	RetryStrategyFixed       RetryStrategy = "fixed"
)

// retryWithBackoff 实现带退避的重试逻辑
func retryWithBackoff(fn func() error, options ErrorHandlingOptions) error {
	var lastErr error

	for attempt := 0; attempt <= options.MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// 检查是否应该重试
		if attempt == options.MaxRetries || !options.ShouldRetry(err) {
			break
		}

		// 计算延迟时间
		delay := calculateDelay(attempt, options)

		log.Printf("Attempt %d failed: %v, retrying in %v", attempt+1, err, delay)
		time.Sleep(delay)
	}

	return fmt.Errorf("operation failed after %d attempts: %w", options.MaxRetries+1, lastErr)
}

// calculateDelay 计算延迟时间
func calculateDelay(attempt int, options ErrorHandlingOptions) time.Duration {
	var delay time.Duration

	switch options.RetryStrategy {
	case RetryStrategyExponential:
		delay = time.Duration(float64(options.RetryDelay) *
			mathPow(options.BackoffMultiplier, float64(attempt)))
	case RetryStrategyLinear:
		delay = options.RetryDelay * time.Duration(attempt+1)
	case RetryStrategyFixed:
		delay = options.RetryDelay
	default:
		delay = options.RetryDelay
	}

	// 限制最大延迟时间
	if delay > options.MaxRetryDelay {
		delay = options.MaxRetryDelay
	}

	// 添加抖动以避免雷群效应
	jitter := time.Duration(rand.Int63n(int64(delay / 2)))
	return delay + jitter
}

// mathPow 简单的幂运算实现
func mathPow(base float64, exp float64) float64 {
	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

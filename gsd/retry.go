package gsd

import (
	"fmt"
	"log"
	"math/rand"
	"time"
)

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

package gsd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sofiworker/gk/gretry"
)

// RetryStrategy 重试策略，定义收敛到 gretry。
type RetryStrategy = gretry.RetryStrategy

const (
	RetryStrategyExponential RetryStrategy = gretry.RetryStrategyExponential
	RetryStrategyLinear      RetryStrategy = gretry.RetryStrategyLinear
	RetryStrategyFixed       RetryStrategy = gretry.RetryStrategyFixed
	RetryStrategyRandom      RetryStrategy = gretry.RetryStrategyRandom
)

// retryWithBackoff 复用 gretry 的退避/抖动/回调实现，gsd 只保留自己的
// 选项结构与默认行为。
func retryWithBackoff(fn func() error, options ErrorHandlingOptions) error {
	shouldRetry := options.ShouldRetry
	if shouldRetry == nil {
		shouldRetry = func(error) bool { return false }
	}

	result := gretry.Do(context.Background(), fn, gretry.ErrorHandlingOptions{
		MaxRetries:        options.MaxRetries,
		RetryDelay:        options.RetryDelay,
		MaxRetryDelay:     options.MaxRetryDelay,
		RetryStrategy:     options.RetryStrategy,
		BackoffMultiplier: options.BackoffMultiplier,
		TransientErrors:   options.TransientErrors,
		ShouldRetry:       shouldRetry,
		Timeout:           options.Timeout,
		OnRetry: func(attempt int, delay time.Duration, err error) {
			log.Printf("Attempt %d failed: %v, retrying in %v", attempt, err, delay)
		},
	})
	if result.Success {
		return nil
	}
	if result.Error == nil {
		result.Error = fmt.Errorf("operation failed after %d attempts", result.Attempts)
	}
	return fmt.Errorf("operation failed after %d attempts: %w", result.Attempts, result.Error)
}

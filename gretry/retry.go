package gretry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// ErrorHandlingOptions 错误处理选项
type ErrorHandlingOptions struct {
	// MaxRetries 最大重试次数
	MaxRetries int `json:"max_retries"`

	// RetryDelay 初始重试延迟
	RetryDelay time.Duration `json:"retry_delay"`

	// MaxRetryDelay 最大重试延迟
	MaxRetryDelay time.Duration `json:"max_retry_delay"`

	// RetryStrategy 重试策略
	RetryStrategy RetryStrategy `json:"retry_strategy"`

	// BackoffMultiplier 退避乘数
	BackoffMultiplier float64 `json:"backoff_multiplier"`

	// JitterType 抖动类型
	JitterType JitterType `json:"jitter_type"`

	// JitterFactor 抖动因子 (0.0 to 1.0)
	JitterFactor float64 `json:"jitter_factor"`

	// TransientErrors 瞬时错误列表
	TransientErrors []string `json:"transient_errors"`

	// ShouldRetry 自定义重试判断函数
	ShouldRetry func(error) bool `json:"-"`

	// Timeout 操作超时时间
	Timeout time.Duration `json:"timeout"`

	// HealthCheckInterval 健康检查间隔
	HealthCheckInterval time.Duration `json:"health_check_interval"`

	// OnRetry 回调函数，在每次重试前调用
	OnRetry func(attempt int, delay time.Duration, err error)

	// OnSuccess 回调函数，在成功后调用
	OnSuccess func(attempt int, elapsed time.Duration)

	// OnFailed 回调函数，在最终失败后调用
	OnFailed func(attempts int, elapsed time.Duration, err error)
}

// DefaultErrorHandlingOptions 默认错误处理选项
var DefaultErrorHandlingOptions = ErrorHandlingOptions{
	MaxRetries:          3,
	RetryDelay:          1 * time.Second,
	MaxRetryDelay:       30 * time.Second,
	RetryStrategy:       RetryStrategyExponential,
	BackoffMultiplier:   2.0,
	JitterType:          JitterNone,
	JitterFactor:        0.0,
	TransientErrors:     []string{"connection refused", "timeout", "unavailable", "etcdserver: leader changed"},
	Timeout:             0, // 默认不设置超时
	HealthCheckInterval: 30 * time.Second,
	ShouldRetry: func(err error) bool {
		if err == nil {
			return false
		}
		// 默认情况下，所有错误都应该重试
		return true
	},
}

// RetryStrategy 重试策略
type RetryStrategy string

const (
	// RetryStrategyExponential 指数退避
	RetryStrategyExponential RetryStrategy = "exponential"

	// RetryStrategyLinear 线性退避
	RetryStrategyLinear RetryStrategy = "linear"

	// RetryStrategyFixed 固定延迟
	RetryStrategyFixed RetryStrategy = "fixed"

	// RetryStrategyRandom 随机延迟
	RetryStrategyRandom RetryStrategy = "random"
)

// JitterType 抖动类型
type JitterType string

const (
	// JitterNone 无抖动
	JitterNone JitterType = "none"

	// JitterFull 全抖动
	JitterFull JitterType = "full"

	// JitterEqual 相等抖动
	JitterEqual JitterType = "equal"

	// JitterDecorrelated 解相关抖动
	JitterDecorrelated JitterType = "decorrelated"
)

// RetryResult 重试结果
type RetryResult struct {
	// Attempts 尝试次数
	Attempts int

	// Elapsed 总耗时
	Elapsed time.Duration

	// Error 最终错误（如果有）
	Error error

	// Success 是否成功
	Success bool
}

// Option 配置选项函数
type Option func(*ErrorHandlingOptions)

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(maxRetries int) Option {
	return func(o *ErrorHandlingOptions) {
		o.MaxRetries = maxRetries
	}
}

// WithRetryDelay 设置初始重试延迟
func WithRetryDelay(delay time.Duration) Option {
	return func(o *ErrorHandlingOptions) {
		o.RetryDelay = delay
	}
}

// WithMaxRetryDelay 设置最大重试延迟
func WithMaxRetryDelay(delay time.Duration) Option {
	return func(o *ErrorHandlingOptions) {
		o.MaxRetryDelay = delay
	}
}

// WithRetryStrategy 设置重试策略
func WithRetryStrategy(strategy RetryStrategy) Option {
	return func(o *ErrorHandlingOptions) {
		o.RetryStrategy = strategy
	}
}

// WithBackoffMultiplier 设置退避乘数
func WithBackoffMultiplier(multiplier float64) Option {
	return func(o *ErrorHandlingOptions) {
		o.BackoffMultiplier = multiplier
	}
}

// WithJitter 设置抖动类型和因子
func WithJitter(jitterType JitterType, factor float64) Option {
	return func(o *ErrorHandlingOptions) {
		o.JitterType = jitterType
		o.JitterFactor = factor
	}
}

// WithTransientErrors 设置瞬时错误列表
func WithTransientErrors(errors []string) Option {
	return func(o *ErrorHandlingOptions) {
		o.TransientErrors = errors
	}
}

// WithShouldRetry 设置自定义重试判断函数
func WithShouldRetry(shouldRetry func(error) bool) Option {
	return func(o *ErrorHandlingOptions) {
		o.ShouldRetry = shouldRetry
	}
}

// WithTimeout 设置操作超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(o *ErrorHandlingOptions) {
		o.Timeout = timeout
	}
}

// WithOnRetry 设置重试回调函数
func WithOnRetry(onRetry func(attempt int, delay time.Duration, err error)) Option {
	return func(o *ErrorHandlingOptions) {
		o.OnRetry = onRetry
	}
}

// WithOnSuccess 设置成功回调函数
func WithOnSuccess(onSuccess func(attempt int, elapsed time.Duration)) Option {
	return func(o *ErrorHandlingOptions) {
		o.OnSuccess = onSuccess
	}
}

// WithOnFailed 设置失败回调函数
func WithOnFailed(onFailed func(attempts int, elapsed time.Duration, err error)) Option {
	return func(o *ErrorHandlingOptions) {
		o.OnFailed = onFailed
	}
}

// NewErrorHandlingOptions 创建新的错误处理选项
func NewErrorHandlingOptions(opts ...Option) ErrorHandlingOptions {
	options := DefaultErrorHandlingOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// Do 执行带重试的操作
func Do(ctx context.Context, fn func() error, options ErrorHandlingOptions) *RetryResult {
	startTime := time.Now()
	var lastErr error

	// 如果设置了超时，则创建带超时的上下文
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	for attempt := 0; attempt <= options.MaxRetries; attempt++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			elapsed := time.Since(startTime)
			result := &RetryResult{
				Attempts: attempt,
				Elapsed:  elapsed,
				Error:    ctx.Err(),
				Success:  false,
			}
			if options.OnFailed != nil {
				options.OnFailed(attempt, elapsed, ctx.Err())
			}
			return result
		default:
		}

		// 执行操作
		err := fn()
		if err == nil {
			// 成功
			elapsed := time.Since(startTime)
			result := &RetryResult{
				Attempts: attempt,
				Elapsed:  elapsed,
				Error:    nil,
				Success:  true,
			}
			if options.OnSuccess != nil {
				options.OnSuccess(attempt, elapsed)
			}
			return result
		}

		lastErr = err

		// 检查是否应该重试
		if attempt == options.MaxRetries || !options.ShouldRetry(err) {
			break
		}

		// 计算延迟时间
		delay := calculateDelay(attempt, options)

		// 调用重试回调
		if options.OnRetry != nil {
			options.OnRetry(attempt+1, delay, err)
		}

		// 等待延迟或上下文取消
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			elapsed := time.Since(startTime)
			result := &RetryResult{
				Attempts: attempt + 1,
				Elapsed:  elapsed,
				Error:    ctx.Err(),
				Success:  false,
			}
			if options.OnFailed != nil {
				options.OnFailed(attempt+1, elapsed, ctx.Err())
			}
			return result
		case <-timer.C:
			// 继续下一次尝试
		}
		timer.Stop()
	}

	// 最终失败
	elapsed := time.Since(startTime)
	result := &RetryResult{
		Attempts: options.MaxRetries + 1,
		Elapsed:  elapsed,
		Error:    fmt.Errorf("operation failed after %d attempts: %w", options.MaxRetries+1, lastErr),
		Success:  false,
	}
	if options.OnFailed != nil {
		options.OnFailed(options.MaxRetries+1, elapsed, result.Error)
	}
	return result
}

// DoWithDefault 使用默认配置执行带重试的操作
func DoWithDefault(ctx context.Context, fn func() error) *RetryResult {
	return Do(ctx, fn, DefaultErrorHandlingOptions)
}

// calculateDelay 计算延迟时间
func calculateDelay(attempt int, options ErrorHandlingOptions) time.Duration {
	var delay time.Duration

	switch options.RetryStrategy {
	case RetryStrategyExponential:
		delay = time.Duration(float64(options.RetryDelay) *
			math.Pow(options.BackoffMultiplier, float64(attempt)))
	case RetryStrategyLinear:
		delay = options.RetryDelay * time.Duration(attempt+1)
	case RetryStrategyFixed:
		delay = options.RetryDelay
	case RetryStrategyRandom:
		// 随机延迟在初始延迟和最大延迟之间
		minDelay := float64(options.RetryDelay)
		maxDelay := float64(options.MaxRetryDelay)
		if maxDelay <= minDelay {
			maxDelay = minDelay * 5 // 默认5倍
		}
		delay = time.Duration(minDelay + rand.Float64()*(maxDelay-minDelay))
	default:
		delay = options.RetryDelay
	}

	// 限制最大延迟时间
	if delay > options.MaxRetryDelay && options.MaxRetryDelay > 0 {
		delay = options.MaxRetryDelay
	}

	// 应用抖动
	delay = applyJitter(delay, options)

	return delay
}

// applyJitter 应用抖动
func applyJitter(delay time.Duration, options ErrorHandlingOptions) time.Duration {
	if options.JitterType == JitterNone || options.JitterFactor <= 0 {
		return delay
	}

	delayFloat := float64(delay)
	//jitter := delayFloat * options.JitterFactor

	switch options.JitterType {
	case JitterFull:
		// 全抖动：在0到delay之间随机
		return time.Duration(rand.Float64() * delayFloat)
	case JitterEqual:
		// 相等抖动：在delay/2到delay之间随机
		min := delayFloat / 2
		return time.Duration(min + rand.Float64()*(delayFloat-min))
	case JitterDecorrelated:
		// 解相关抖动：在delay到3*delay之间随机
		max := delayFloat * 3
		return time.Duration(delayFloat + rand.Float64()*(max-delayFloat))
	default:
		return delay
	}
}

# gretry - 增强重试包 / Enhanced Retry Package

`gretry` 为 Go 应用提供完整且灵活的重试机制：多种重试策略、可配置退避算法、抖动选项与回调钩子。
`gretry` provides a comprehensive and flexible retry mechanism: multiple strategies, configurable backoff, jitter options and callback hooks.

## 特性 / Features

- 多种重试策略（指数、线性、固定、随机）；multiple strategies (exponential, linear, fixed, random)
- 可配置退避乘数；configurable backoff multiplier
- 抖动支持（full、equal、decorrelated）；jitter support
- 感知 context 取消；context-aware cancellation
- 可自定义重试条件；customizable retry conditions
- 回调钩子（重试前、成功后、最终失败）；callback hooks
- 详细的结果统计与上报；detailed result metrics
- 超时支持；timeout support

## 安装 / Installation

```bash
go get github.com/sofiworker/gk/gretry
```

## 用法 / Usage

### 基础用法 / Basic Usage

```go
result := gretry.DoWithDefault(context.Background(), func() error {
	// 你的操作 / your operation
	return nil // 或返回 error 触发重试 / or return an error to retry
})

if result.Success {
	fmt.Printf("Operation succeeded after %d attempts\n", result.Attempts)
} else {
	fmt.Printf("Operation failed: %v\n", result.Error)
}
```

### 自定义配置 / Custom Configuration

```go
options := gretry.NewErrorHandlingOptions(
	gretry.WithMaxRetries(5),
	gretry.WithRetryDelay(1*time.Second),
	gretry.WithMaxRetryDelay(30*time.Second),
	gretry.WithRetryStrategy(gretry.RetryStrategyExponential),
	gretry.WithBackoffMultiplier(2.0),
	gretry.WithJitter(gretry.JitterEqual, 0.1),
	gretry.WithShouldRetry(func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "temporary")
	}),
)

result := gretry.Do(context.Background(), func() error { return nil }, options)
```

### context 取消 / Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

result := gretry.Do(ctx, func() error {
	time.Sleep(2 * time.Second)
	return nil
}, gretry.DefaultErrorHandlingOptions)
```

## 配置项 / Configuration Options

| 配置 / Option | 说明 / Description | 默认值 / Default |
|------|------|--------|
| `MaxRetries` | 最大重试次数 / max retries | 3 |
| `RetryDelay` | 初始延迟 / initial delay | 1 秒 |
| `MaxRetryDelay` | 最大延迟 / max delay | 30 秒 |
| `RetryStrategy` | 延迟策略 / strategy | 指数 / exponential |
| `BackoffMultiplier` | 指数退避乘数 / multiplier | 2.0 |
| `JitterType` | 抖动类型 / jitter type | 无 / none |
| `JitterFactor` | 抖动因子 / jitter factor | 0.0 |
| `Timeout` | 整体超时 / overall timeout | 10 秒 |

更多用法见 `gretry` 包测试（`retry_test.go`）。
More usage examples are in `gretry`'s tests (`retry_test.go`).

# gretry - 增强重试包

[English](README.en.md) | 中文

`gretry` 为 Go 应用提供完整且灵活的重试机制：多种重试策略、可配置退避算法、抖动选项与回调钩子。

## 特性

- 多种重试策略（指数、线性、固定、随机）
- 可配置退避乘数
- 抖动支持（full、equal、decorrelated）
- 感知 context 取消
- 可自定义重试条件
- 回调钩子（重试前、成功后、最终失败）
- 详细的结果统计与上报
- 超时支持

## 安装

```bash
go get github.com/sofiworker/gk/gretry
```

## 用法

### 基础用法

```go
result := gretry.DoWithDefault(context.Background(), func() error {
	// 你的操作
	return nil // 或返回 error 触发重试
})

if result.Success {
	fmt.Printf("Operation succeeded after %d attempts\n", result.Attempts)
} else {
	fmt.Printf("Operation failed: %v\n", result.Error)
}
```

### 自定义配置

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

### context 取消

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

result := gretry.Do(ctx, func() error {
	time.Sleep(2 * time.Second)
	return nil
}, gretry.DefaultErrorHandlingOptions)
```

## 配置项

| 配置 | 说明 | 默认值 |
|------|------|--------|
| `MaxRetries` | 最大重试次数 | 3 |
| `RetryDelay` | 初始延迟 | 1 秒 |
| `MaxRetryDelay` | 最大延迟 | 30 秒 |
| `RetryStrategy` | 延迟策略 | 指数 |
| `BackoffMultiplier` | 指数退避乘数 | 2.0 |
| `JitterType` | 抖动类型 | 无 |
| `JitterFactor` | 抖动因子 | 0.0 |
| `Timeout` | 整体超时 | 10 秒 |

更多用法见 `gretry` 包测试（`retry_test.go`）。

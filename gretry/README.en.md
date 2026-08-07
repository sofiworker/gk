# gretry - Enhanced Retry Package

English | [中文](README.md)

`gretry` provides a comprehensive and flexible retry mechanism: multiple strategies, configurable backoff, jitter options and callback hooks.

## Features

- Multiple retry strategies (exponential, linear, fixed, random)
- Configurable backoff multiplier
- Jitter support (full, equal, decorrelated)
- Context-aware cancellation
- Customizable retry conditions
- Callback hooks (on retry, on success, on failure)
- Detailed result metrics
- Timeout support

## Installation

```bash
go get github.com/sofiworker/gk/gretry
```

## Usage

### Basic Usage

```go
result := gretry.DoWithDefault(context.Background(), func() error {
	// your operation
	return nil // or return an error to retry
})

if result.Success {
	fmt.Printf("Operation succeeded after %d attempts\n", result.Attempts)
} else {
	fmt.Printf("Operation failed: %v\n", result.Error)
}
```

### Custom Configuration

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

### Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

result := gretry.Do(ctx, func() error {
	time.Sleep(2 * time.Second)
	return nil
}, gretry.DefaultErrorHandlingOptions)
```

## Configuration Options

| Option | Description | Default |
|------|------|--------|
| `MaxRetries` | max retries | 3 |
| `RetryDelay` | initial delay | 1 second |
| `MaxRetryDelay` | max delay | 30 seconds |
| `RetryStrategy` | strategy | exponential |
| `BackoffMultiplier` | exponential multiplier | 2.0 |
| `JitterType` | jitter type | none |
| `JitterFactor` | jitter factor | 0.0 |
| `Timeout` | overall timeout | 10 seconds |

More usage examples are in `gretry`'s tests (`retry_test.go`).

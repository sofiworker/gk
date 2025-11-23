# gretry - Enhanced Retry Package

The `gretry` package provides a comprehensive and flexible retry mechanism for Go applications. It supports various retry strategies, configurable backoff algorithms, jitter options, and callback hooks.

## Features

- Multiple retry strategies (exponential, linear, fixed, random)
- Configurable backoff with multiplier
- Jitter support (full, equal, decorrelated)
- Context-aware cancellation
- Customizable retry conditions
- Callback hooks (on retry, on success, on failure)
- Detailed metrics and reporting
- Timeout support

## Installation

```bash
go get github.com/sofiworker/gk/gretry
```

## Usage

### Basic Usage

```go
import "github.com/sofiworker/gk/gretry"

// Simple retry with default options
result := gretry.DoWithDefault(context.Background(), func() error {
    // Your operation here
    return nil // or return an error to trigger retry
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
    gretry.WithRetryDelay(1 * time.Second),
    gretry.WithMaxRetryDelay(30 * time.Second),
    gretry.WithRetryStrategy(gretry.RetryStrategyExponential),
    gretry.WithBackoffMultiplier(2.0),
    gretry.WithJitter(gretry.JitterEqual, 0.1),
    gretry.WithShouldRetry(func(err error) bool {
        // Custom retry logic
        return err != nil && strings.Contains(err.Error(), "temporary")
    }),
)

result := gretry.Do(context.Background(), func() error {
    // Your operation here
    return nil
}, options)
```

### With Callbacks

```go
options := gretry.NewErrorHandlingOptions(
    gretry.WithMaxRetries(3),
    gretry.WithOnRetry(func(attempt int, delay time.Duration, err error) {
        log.Printf("Retrying attempt %d in %v due to: %v", attempt, delay, err)
    }),
    gretry.WithOnSuccess(func(attempt int, elapsed time.Duration) {
        log.Printf("Succeeded on attempt %d after %v", attempt, elapsed)
    }),
    gretry.WithOnFailed(func(attempts int, elapsed time.Duration, err error) {
        log.Printf("Failed after %d attempts over %v: %v", attempts, elapsed, err)
    }),
)

result := gretry.Do(context.Background(), func() error {
    // Your operation here
    return nil
}, options)
```

### With Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

result := gretry.Do(ctx, func() error {
    // Long-running operation
    time.Sleep(2 * time.Second)
    return nil
}, gretry.DefaultErrorHandlingOptions)
```

## Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `MaxRetries` | Maximum number of retry attempts | 3 |
| `RetryDelay` | Initial delay between retries | 1 second |
| `MaxRetryDelay` | Maximum delay between retries | 30 seconds |
| `RetryStrategy` | Strategy for calculating delays | Exponential |
| `BackoffMultiplier` | Multiplier for exponential backoff | 2.0 |
| `JitterType` | Type of jitter to apply | None |
| `JitterFactor` | Factor for jitter (0.0 to 1.0) | 0.0 |
| `Timeout` | Overall timeout for the operation | 10 seconds |

## Retry Strategies

- `RetryStrategyExponential`: Exponential backoff (default)
- `RetryStrategyLinear`: Linear backoff
- `RetryStrategyFixed`: Fixed delay
- `RetryStrategyRandom`: Random delay between min and max

## Jitter Types

- `JitterNone`: No jitter (default)
- `JitterFull`: Full jitter (0 to delay)
- `JitterEqual`: Equal jitter (delay/2 to delay)
- `JitterDecorrelated`: Decorrelated jitter (delay to 3*delay)

## Examples

See the [example](../example/retry_example.go) directory for comprehensive usage examples.
//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sofiworker/gk/gretry"
)

func main() {
	// Example 1: Basic retry with default options
	fmt.Println("=== Example 1: Basic retry ===")
	basicRetryExample()

	// Example 2: Customized retry configuration
	fmt.Println("\n=== Example 2: Customized retry ===")
	customRetryExample()

	// Example 3: Retry with context cancellation
	fmt.Println("\n=== Example 3: Retry with context ===")
	contextRetryExample()

	// Example 4: Retry with timeout option
	fmt.Println("\n=== Example 4: Retry with timeout ===")
	timeoutRetryExample()

	// Example 5: Retry with callbacks
	fmt.Println("\n=== Example 5: Retry with callbacks ===")
	callbackRetryExample()
}

// basicRetryExample demonstrates basic retry usage
func basicRetryExample() {
	attempts := 0
	fn := func() error {
		attempts++
		fmt.Printf("Attempt %d\n", attempts)
		if attempts < 3 {
			return fmt.Errorf("simulated error on attempt %d", attempts)
		}
		fmt.Println("Operation succeeded!")
		return nil
	}

	result := gretry.DoWithDefault(context.Background(), fn)
	if result.Success {
		fmt.Printf("Success after %d attempts\n", result.Attempts)
	} else {
		fmt.Printf("Failed after %d attempts: %v\n", result.Attempts, result.Error)
	}
}

// customRetryExample demonstrates customized retry configuration
func customRetryExample() {
	// Create custom retry options
	options := gretry.NewErrorHandlingOptions(
		gretry.WithMaxRetries(5),
		gretry.WithRetryDelay(500*time.Millisecond),
		gretry.WithMaxRetryDelay(5*time.Second),
		gretry.WithRetryStrategy(gretry.RetryStrategyExponential),
		gretry.WithBackoffMultiplier(1.5),
		gretry.WithJitter(gretry.JitterEqual, 0.2),
		gretry.WithShouldRetry(func(err error) bool {
			// Only retry on network errors
			return err != nil && (err.Error() == "network error" || err.Error() == "timeout")
		}),
	)

	attempts := 0
	fn := func() error {
		attempts++
		fmt.Printf("Custom attempt %d\n", attempts)
		switch attempts {
		case 1:
			return fmt.Errorf("network error")
		case 2:
			return fmt.Errorf("timeout")
		case 3:
			return fmt.Errorf("permanent error") // This won't be retried
		default:
			fmt.Println("Custom operation succeeded!")
			return nil
		}
	}

	result := gretry.Do(context.Background(), fn, options)
	if result.Success {
		fmt.Printf("Custom success after %d attempts\n", result.Attempts)
	} else {
		fmt.Printf("Custom failed after %d attempts: %v\n", result.Attempts, result.Error)
	}
}

// contextRetryExample demonstrates retry with context cancellation
func contextRetryExample() {
	// Create a context that cancels after 3 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	attempts := 0
	fn := func() error {
		attempts++
		fmt.Printf("Context attempt %d\n", attempts)
		// Simulate a long-running operation
		time.Sleep(2 * time.Second)
		return fmt.Errorf("simulated error")
	}

	result := gretry.Do(ctx, fn, gretry.DefaultErrorHandlingOptions)
	if result.Success {
		fmt.Printf("Context success after %d attempts\n", result.Attempts)
	} else {
		fmt.Printf("Context failed after %d attempts: %v\n", result.Attempts, result.Error)
	}
}

// timeoutRetryExample demonstrates retry with timeout option
func timeoutRetryExample() {
	// Create options with a 1-second timeout
	options := gretry.NewErrorHandlingOptions(
		gretry.WithTimeout(1*time.Second),
		gretry.WithMaxRetries(3),
	)

	fn := func() error {
		fmt.Println("Operation started, sleeping for 2 seconds...")
		time.Sleep(2 * time.Second) // Longer than timeout
		fmt.Println("Operation completed")
		return nil
	}

	result := gretry.Do(context.Background(), fn, options)
	if result.Success {
		fmt.Printf("Timeout success after %d attempts\n", result.Attempts)
	} else {
		fmt.Printf("Timeout failed after %d attempts: %v\n", result.Attempts, result.Error)
	}
}

// callbackRetryExample demonstrates retry with callbacks
func callbackRetryExample() {
	options := gretry.NewErrorHandlingOptions(
		gretry.WithMaxRetries(3),
		gretry.WithRetryDelay(1*time.Second),
		gretry.WithOnRetry(func(attempt int, delay time.Duration, err error) {
			log.Printf("Retrying attempt %d in %v due to error: %v", attempt, delay, err)
		}),
		gretry.WithOnSuccess(func(attempt int, elapsed time.Duration) {
			log.Printf("Operation succeeded on attempt %d after %v", attempt, elapsed)
		}),
		gretry.WithOnFailed(func(attempts int, elapsed time.Duration, err error) {
			log.Printf("Operation failed after %d attempts over %v: %v", attempts, elapsed, err)
		}),
	)

	attempts := 0
	fn := func() error {
		attempts++
		fmt.Printf("Callback attempt %d\n", attempts)
		if attempts < 4 {
			return fmt.Errorf("callback error %d", attempts)
		}
		fmt.Println("Callback operation succeeded!")
		return nil
	}

	result := gretry.Do(context.Background(), fn, options)
	fmt.Printf("Callback result: Success=%v, Attempts=%d, Error=%v\n",
		result.Success, result.Attempts, result.Error)
}

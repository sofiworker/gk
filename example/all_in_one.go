package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sofiworker/gk/gcache"
	"github.com/sofiworker/gk/gcompress"
	"github.com/sofiworker/gk/gconfig"
	"github.com/sofiworker/gk/glog"
	"github.com/sofiworker/gk/gretry"
)

// This example demonstrates the usage of multiple gk packages together.
func main() {
	// 1. Configure Logging
	glog.Configure(glog.WithLevel(glog.InfoLevel))
	glog.Info("Starting all-in-one example")

	// 2. Load Configuration
	type AppConfig struct {
		AppName string `json:"app_name"`
		Port    int    `json:"port"`
	}
	// Create a dummy config file
	// ... (In real app, this would be a file)
	// Here we just rely on defaults or env
	
	// 3. Use Cache
	cache := gcache.NewMemoryCache(time.Minute)
	ctx := context.Background()
	_ = cache.Set(ctx, "key", []byte("value"), 0)
	val, _ := cache.Get(ctx, "key")
	glog.Infof("Cache value: %s", string(val))

	// 4. Retry Operation
	opts := gretry.NewErrorHandlingOptions(
		gretry.WithMaxRetries(3),
		gretry.WithRetryDelay(100*time.Millisecond),
	)
	
	err := gretry.DoWithDefault(ctx, func() error {
		glog.Info("Doing some work...")
		return nil
	}).Error
	
	if err != nil {
		glog.Errorf("Work failed: %v", err)
	}

	// 5. Compress Data
	compressed, _ := gcompress.CompressString("hello world")
	glog.Infof("Compressed size: %d", len(compressed))

	glog.Info("Example finished successfully")
}

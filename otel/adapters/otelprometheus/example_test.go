package otelprometheus_test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sofiworker/keel/otel"
	oteladapter "github.com/sofiworker/keel/otel/adapters/otel"
	"github.com/sofiworker/keel/otel/adapters/otelprometheus"
)

func ExampleNew() {
	ctx := context.Background()
	provider, err := otelprometheus.New(ctx,
		otelprometheus.WithProviderOptions(oteladapter.WithServiceName("orders")),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	counter, err := provider.Counter("http.requests", gotel.WithAttributeKeys("method"))
	if err == nil {
		counter.Increment(ctx, gotel.KV("method", "GET"))
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", provider.Handler())
	// Serve mux using the application's HTTP server.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := provider.Shutdown(shutdownCtx); err != nil {
		fmt.Println(err)
	}
}

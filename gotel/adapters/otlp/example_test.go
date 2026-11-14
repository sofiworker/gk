package otlp_test

import (
	"context"
	"fmt"
	"time"

	oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
	"github.com/sofiworker/gk/gotel/adapters/otlp"
)

func ExampleNew_jaeger() {
	ctx := context.Background()
	provider, err := otlp.New(ctx,
		otlp.WithProtocol(otlp.ProtocolGRPC),
		otlp.WithEndpointURL("http://localhost:4317"),
		otlp.WithMetrics(false),
		otlp.WithProviderOptions(oteladapter.WithServiceName("orders"), oteladapter.WithSamplingRatio(0.1)),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	_, span := provider.Start(ctx, "create-order")
	span.End()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := provider.Shutdown(shutdownCtx); err != nil {
		fmt.Println(err)
	}
}

func ExampleNew_collector() {
	provider, err := otlp.New(context.Background(),
		otlp.WithEndpointURL("https://collector.example.com:4318"),
		otlp.WithMetricInterval(30*time.Second),
		otlp.WithProviderOptions(oteladapter.WithServiceName("orders")),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	counter, err := provider.Counter("orders")
	if err == nil {
		counter.Increment(context.Background())
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := provider.Shutdown(shutdownCtx); err != nil {
		fmt.Println(err)
	}
}

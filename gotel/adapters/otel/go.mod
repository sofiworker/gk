module github.com/sofiworker/gk/gotel/adapters/otel

go 1.25.12

require (
	github.com/sofiworker/gk v0.0.0
	go.opentelemetry.io/otel v1.38.0
	go.opentelemetry.io/otel/metric v1.38.0
	go.opentelemetry.io/otel/sdk v1.38.0
	go.opentelemetry.io/otel/sdk/metric v1.38.0
	go.opentelemetry.io/otel/trace v1.38.0
)

require (
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace github.com/sofiworker/gk => ../../..

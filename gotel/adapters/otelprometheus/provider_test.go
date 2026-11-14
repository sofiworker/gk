package otelprometheus

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sofiworker/gk/gotel"
	oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
	otelexport "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
)

func TestProviderScrapeAndLifecycle(t *testing.T) {
	ctx := context.Background()
	registry := prom.NewRegistry()
	existing := prom.NewGauge(prom.GaugeOpts{Name: "existing", Help: "existing"})
	if err := registry.Register(existing); err != nil {
		t.Fatal(err)
	}
	existing.Set(9)
	p, err := New(ctx, nil, WithRegistry(registry),
		WithExporterOptions(otelexport.WithNamespace("demo"), otelexport.WithoutScopeInfo()),
		WithHandlerOptions(promhttp.HandlerOpts{EnableOpenMetrics: true}),
		WithProviderOptions(oteladapter.WithServiceName("checkout"), oteladapter.WithServiceVersion("1.2.3"),
			oteladapter.WithResourceAttributes(gotel.KV("deployment.environment.name", "test"))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if p.Registry() != registry {
		t.Fatal("registry was not preserved")
	}
	counter, err := p.Counter("http.requests", gotel.WithAttributeKeys("method"))
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(ctx, 2, gotel.KV("method", "GET"))
	counter.Increment(ctx, gotel.KV("method", "GET"))
	active, err := p.UpDownCounter("http.active")
	if err != nil {
		t.Fatal(err)
	}
	active.Add(ctx, 1)
	active.Add(ctx, -1)
	histogram, err := p.Histogram("http.duration", gotel.WithUnit("s"), gotel.WithHistogramBoundaries(0.1, 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	histogram.Record(ctx, 1.5)
	gauge, err := p.Gauge("http.body", gotel.WithUnit("By"))
	if err != nil {
		t.Fatal(err)
	}
	gauge.Record(ctx, 22)
	_, err = p.MeterProvider().Meter("native").Float64ObservableGauge("async", metric.WithFloat64Callback(func(_ context.Context, observer metric.Float64Observer) error {
		observer.Observe(7)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/metrics", nil)
	request.Header.Set("Accept", "application/openmetrics-text; version=1.0.0; charset=utf-8")
	response := httptest.NewRecorder()
	p.Handler().ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Type"), "application/openmetrics-text") {
		t.Fatalf("response = %d/%s", response.Code, response.Body)
	}
	body := response.Body.String()
	for _, want := range []string{"demo_http_requests_total{method=\"GET\"} 3", "demo_http_active 0", "demo_http_duration_seconds_sum 1.5", "demo_http_body_bytes 22", "demo_async 7", "service_name=\"checkout\"", "service_version=\"1.2.3\"", "deployment_environment_name=\"test\"", "existing 9", "# EOF"} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %q:\n%s", want, body)
		}
	}
	// The shared registry holds only application collectors, even while OTel
	// metrics are being served. OTel's unchecked collector must never leak here.
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 1 || families[0].GetName() != "existing" {
		t.Fatalf("shared registry was modified: %v", families)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := p.Shutdown(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	families, err = registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 1 || families[0].GetName() != "existing" {
		t.Fatalf("collectors after shutdown = %v", families)
	}
	response = httptest.NewRecorder()
	p.Handler().ServeHTTP(response, request)
	if response.Code != 200 || strings.Contains(response.Body.String(), "demo_http_requests_total") || !strings.Contains(response.Body.String(), "existing 9") {
		t.Fatalf("scrape after shutdown = %d/%s", response.Code, response.Body)
	}
}

func TestProviderConstructionAndFailureCleanup(t *testing.T) {
	if _, err := New(context.Background(), WithRegistry(nil)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil registry = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled construction = %v", err)
	}
	registry := prom.NewRegistry()
	if _, err := New(context.Background(), WithRegistry(registry), WithProviderOptions(oteladapter.WithResource(nil))); !errors.Is(err, oteladapter.ErrInvalidConfig) {
		t.Fatalf("provider error = %v", err)
	}
	p, err := New(context.Background(), WithRegistry(registry))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	isolated, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if isolated.Registry() == nil || isolated.Registry() == registry {
		t.Fatal("default registry is not isolated")
	}
	if err := isolated.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProviderReportsConflictingApplicationMetric(t *testing.T) {
	registry := prom.NewRegistry()
	counter := prom.NewCounter(prom.CounterOpts{Name: "requests_total", Help: "application requests"})
	if err := registry.Register(counter); err != nil {
		t.Fatal(err)
	}
	counter.Inc()
	p, err := New(context.Background(), WithRegistry(registry), WithExporterOptions(otelexport.WithoutScopeInfo()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	otelCounter, err := p.Counter("requests")
	if err != nil {
		t.Fatal(err)
	}
	otelCounter.Increment(context.Background())
	response := httptest.NewRecorder()
	p.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if response.Code != 500 || !strings.Contains(response.Body.String(), "requests_total") {
		t.Fatalf("conflict response = %d/%s", response.Code, response.Body)
	}
}

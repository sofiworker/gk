package otel

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/gotel"
	api "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type spanSink struct {
	mu          sync.Mutex
	spans       []sdktrace.ReadOnlySpan
	shutdowns   int
	exportErr   error
	shutdownErr error
}

func (s *spanSink) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spans = append(s.spans, spans...)
	return s.exportErr
}
func (s *spanSink) Shutdown(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdowns++
	return s.shutdownErr
}

type metricSink struct {
	mu          sync.Mutex
	data        metricdata.ResourceMetrics
	shutdowns   int
	exportErr   error
	shutdownErr error
}

func (*metricSink) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(kind)
}
func (*metricSink) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}
func (s *metricSink) Export(_ context.Context, data *metricdata.ResourceMetrics) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = *data
	return s.exportErr
}
func (*metricSink) ForceFlush(context.Context) error { return nil }
func (s *metricSink) Shutdown(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdowns++
	return s.shutdownErr
}

func TestProviderExportsResourcesAndNativeCapabilities(t *testing.T) {
	ctx := context.Background()
	spans, metrics := &spanSink{}, &metricSink{}
	reader := sdkmetric.NewPeriodicReader(metrics, sdkmetric.WithInterval(time.Hour))
	globalTracer, globalMeter := api.GetTracerProvider(), api.GetMeterProvider()
	attrs := []gotel.KeyValue{gotel.KV("deployment.environment.name", "test")}
	resourceOption := WithResourceAttributes(attrs...)
	attrs[0].Value = "mutated"
	p, err := NewProvider(ctx, nil, WithServiceName("checkout"), WithServiceVersion("1.2.3"),
		WithResource(resource.NewSchemaless(attribute.String("custom", "value"))), resourceOption,
		WithInstrumentationName("example"), WithTraceExporter(spans), WithMetricReader(reader),
		WithBatchOptions(sdktrace.WithBatchTimeout(time.Hour)),
		WithMetricOptions(sdkmetric.WithView(sdkmetric.NewView(sdkmetric.Instrument{Name: "raw"}, sdkmetric.Stream{Name: "renamed"}))),
		WithProviderPropagator(propagation.TraceContext{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if globalTracer != api.GetTracerProvider() || globalMeter != api.GetMeterProvider() {
		t.Fatal("global providers changed")
	}
	_, span := p.Start(ctx, "request")
	span.End()
	_, linked := p.TracerProvider().Tracer("native").Start(ctx, "linked", trace.WithLinks(trace.Link{SpanContext: trace.SpanContextFromContext(contextWithSpan(t, p))}))
	linked.End()
	counter, err := p.Counter("raw")
	if err != nil {
		t.Fatal(err)
	}
	counter.Increment(ctx)
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
	spans.mu.Lock()
	defer spans.mu.Unlock()
	if len(spans.spans) != 3 {
		t.Fatalf("exported spans = %d", len(spans.spans))
	}
	var hasLink bool
	for _, exported := range spans.spans {
		if exported.Name() == "linked" {
			hasLink = len(exported.Links()) == 1
		}
		resourceAttrs := exported.Resource().Set()
		for key, want := range map[string]string{"service.name": "checkout", "service.version": "1.2.3", "deployment.environment.name": "test", "custom": "value"} {
			got, ok := resourceAttrs.Value(attribute.Key(key))
			if !ok || got.AsString() != want {
				t.Fatalf("resource %s = %v", key, got)
			}
		}
	}
	if !hasLink {
		t.Fatal("native span link was lost")
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	names := map[string]bool{}
	for _, scope := range metrics.data.ScopeMetrics {
		for _, m := range scope.Metrics {
			names[m.Name] = true
		}
	}
	if !names["renamed"] || !names["async"] {
		t.Fatalf("exported native/view metrics = %v", names)
	}
	if value, ok := metrics.data.Resource.Set().Value("service.name"); !ok || value.AsString() != "checkout" {
		t.Fatalf("metric resource = %v", metrics.data.Resource)
	}
}

func contextWithSpan(t *testing.T, p *Provider) context.Context {
	t.Helper()
	ctx, span := p.Start(context.Background(), "parent")
	span.End()
	return ctx
}

func TestProviderSampling(t *testing.T) {
	for _, override := range []bool{false, true} {
		sink := &spanSink{}
		opts := []ProviderOption{WithSamplingRatio(0), WithTraceExporter(sink)}
		if override {
			opts = append(opts, WithSampler(sdktrace.AlwaysSample()))
		}
		p, err := NewProvider(context.Background(), opts...)
		if err != nil {
			t.Fatal(err)
		}
		_, span := p.Start(context.Background(), "root")
		if span.Context().IsSampled() != override {
			t.Fatal("sampler override or ratio was ignored")
		}
		span.End()
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		sink.mu.Lock()
		got := len(sink.spans)
		sink.mu.Unlock()
		want := 0
		if override {
			want = 1
		}
		if got != want {
			t.Fatalf("sampled spans = %d, want %d", got, want)
		}
	}
}

func TestProviderFlushAndShutdownErrors(t *testing.T) {
	traceErr, metricErr := errors.New("trace failed"), errors.New("metric failed")
	spans := &spanSink{exportErr: traceErr, shutdownErr: traceErr}
	metrics := &metricSink{exportErr: metricErr, shutdownErr: metricErr}
	p, err := NewProvider(context.Background(), WithTraceExporter(spans), WithMetricReader(sdkmetric.NewPeriodicReader(metrics, sdkmetric.WithInterval(time.Hour))))
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Start(context.Background(), "error")
	span.End()
	counter, err := p.Counter("requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Increment(context.Background())
	if err := p.ForceFlush(context.Background()); !errors.Is(err, traceErr) || !errors.Is(err, metricErr) {
		t.Fatalf("flush error = %v", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := p.Shutdown(context.Background()); !errors.Is(err, traceErr) || !errors.Is(err, metricErr) {
				t.Errorf("shutdown error = %v", err)
			}
		})
	}
	wg.Wait()
	if spans.shutdowns != 1 || metrics.shutdowns != 1 {
		t.Fatalf("shutdown counts = %d/%d", spans.shutdowns, metrics.shutdowns)
	}
}

func TestProviderInvalidConfiguration(t *testing.T) {
	for _, opt := range []ProviderOption{WithResource(nil), WithInstrumentationName(" "), WithSamplingRatio(-1), WithSamplingRatio(2), WithSamplingRatio(math.NaN()), WithMetricReader(nil), WithProviderPropagator(nil)} {
		if _, err := NewProvider(context.Background(), opt); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("invalid config error = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewProvider(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled construction = %v", err)
	}
	p, err := NewProvider(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProviderCanceledShutdownStillClosesExporter(t *testing.T) {
	sink := &spanSink{}
	p, err := NewProvider(context.Background(), WithTraceExporter(sink))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown error = %v", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.shutdowns != 1 {
		t.Fatalf("exporter shutdown count = %d", sink.shutdowns)
	}
}

func TestProviderShutdownReturnsPendingTraceExportError(t *testing.T) {
	want := errors.New("pending trace export failed")
	sink := &spanSink{exportErr: want}
	p, err := NewProvider(context.Background(), WithTraceExporter(sink), WithBatchOptions(sdktrace.WithBatchTimeout(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Start(context.Background(), "pending")
	span.End()
	if err := p.Shutdown(context.Background()); !errors.Is(err, want) {
		t.Fatalf("shutdown error = %v", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.shutdowns != 1 || len(sink.spans) != 1 {
		t.Fatalf("shutdowns/exports = %d/%d", sink.shutdowns, len(sink.spans))
	}
}

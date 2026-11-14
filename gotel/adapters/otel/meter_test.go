package otel

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/sofiworker/gk/gotel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMeterRecords(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	m, err := NewMeter(provider.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	opts := []gotel.InstrumentOption{gotel.WithAttributeKeys("method", "route")}
	counter, err := m.Counter("requests", opts...)
	if err != nil {
		t.Fatal(err)
	}
	counter.Increment(ctx, gotel.KV("method", "GET"), gotel.KV("extra", "ignored"))
	counter.Add(ctx, 2, gotel.KV("method", "GET"))
	active, err := m.UpDownCounter("active", opts...)
	if err != nil {
		t.Fatal(err)
	}
	active.Add(ctx, 2)
	active.Add(ctx, -1)
	histogram, err := m.Histogram("duration", gotel.WithUnit("s"), gotel.WithDescription("duration"), gotel.WithHistogramBoundaries(0.1, 1))
	if err != nil {
		t.Fatal(err)
	}
	histogram.Record(ctx, 0.5)
	gauge, err := m.Gauge("temperature")
	if err != nil {
		t.Fatal(err)
	}
	gauge.Record(ctx, 20)
	gauge.Record(ctx, 21)
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		counter.Add(ctx, invalid)
		active.Add(ctx, invalid)
		histogram.Record(ctx, invalid)
		gauge.Record(ctx, invalid)
	}
	counter.Add(ctx, -1)
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	metrics := map[string]metricdata.Metrics{}
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			metrics[metric.Name] = metric
		}
	}
	sum := metrics["requests"].Data.(metricdata.Sum[float64])
	if !sum.IsMonotonic || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 3 {
		t.Fatalf("counter = %+v", sum)
	}
	want := attribute.NewSet(attribute.String("method", "GET"), attribute.String("route", ""))
	if !sum.DataPoints[0].Attributes.Equals(&want) {
		t.Fatalf("attributes = %v", sum.DataPoints[0].Attributes)
	}
	upDown := metrics["active"].Data.(metricdata.Sum[float64])
	if upDown.IsMonotonic || upDown.DataPoints[0].Value != 1 {
		t.Fatalf("up-down counter = %+v", upDown)
	}
	hist := metrics["duration"].Data.(metricdata.Histogram[float64])
	if hist.DataPoints[0].Count != 1 || hist.DataPoints[0].Sum != 0.5 || !reflect.DeepEqual(hist.DataPoints[0].Bounds, []float64{0.1, 1}) {
		t.Fatalf("histogram = %+v", hist)
	}
	if metrics["duration"].Unit != "s" || metrics["duration"].Description != "duration" {
		t.Fatalf("metadata = %+v", metrics["duration"])
	}
	if got := metrics["temperature"].Data.(metricdata.Gauge[float64]).DataPoints[0].Value; got != 21 {
		t.Fatalf("gauge = %v", got)
	}
}

func TestMeterConcurrentRecording(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	m, err := NewMeter(provider.Meter("concurrent"))
	if err != nil {
		t.Fatal(err)
	}
	counter, err := m.Counter("requests")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				counter.Increment(ctx)
			}
		})
	}
	wg.Wait()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	if got := data.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[float64]).DataPoints[0].Value; got != 1600 {
		t.Fatalf("counter = %v", got)
	}
}

var errSDK = errors.New("SDK creation failed")

type failingMeter struct{ metric.Meter }

func (failingMeter) Float64Counter(string, ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	return nil, errSDK
}
func (failingMeter) Float64UpDownCounter(string, ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	return nil, errSDK
}
func (failingMeter) Float64Histogram(string, ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return nil, errSDK
}
func (failingMeter) Float64Gauge(string, ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	return nil, errSDK
}

func TestMeterErrors(t *testing.T) {
	if _, err := NewMeter(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil meter: %v", err)
	}
	m, err := NewMeter(failingMeter{})
	if err != nil {
		t.Fatal(err)
	}
	creators := []func(string) error{
		func(name string) error { _, err := m.Counter(name); return err },
		func(name string) error { _, err := m.UpDownCounter(name); return err },
		func(name string) error { _, err := m.Histogram(name); return err },
		func(name string) error { _, err := m.Gauge(name); return err },
	}
	for _, create := range creators {
		if err := create(""); !errors.Is(err, gotel.ErrInvalidInstrument) {
			t.Fatalf("config error = %v", err)
		}
		if err := create("metric"); !errors.Is(err, errSDK) {
			t.Fatalf("SDK error = %v", err)
		}
	}
}

func TestAttributes(t *testing.T) {
	values := []any{"string", true, 1, int32(2), int64(3), float32(1.5), 2.5, []string{"a"}, []bool{true}, []int{1}, []int64{2}, []float64{3}, uint64(4)}
	want := []any{"string", true, int64(1), int64(2), int64(3), 1.5, 2.5, []string{"a"}, []bool{true}, []int64{1}, []int64{2}, []float64{3}, "4"}
	for i, value := range values {
		got := attributes([]gotel.KeyValue{gotel.KV("key", value)})
		if len(got) != 1 || string(got[0].Key) != "key" || !reflect.DeepEqual(got[0].Value.AsInterface(), want[i]) {
			t.Fatalf("attributes(%#v) = %#v", value, got)
		}
	}
}

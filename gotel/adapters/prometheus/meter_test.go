package prometheus

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	prom "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sofiworker/gk/gotel"
)

func newTestMeter(t *testing.T) (*Meter, *prom.Registry) {
	t.Helper()
	registry := prom.NewPedanticRegistry()
	meter, err := NewMeter(registry)
	if err != nil {
		t.Fatal(err)
	}
	return meter, registry
}

func gather(t *testing.T, registry *prom.Registry) map[string]*dto.MetricFamily {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		result[family.GetName()] = family
	}
	return result
}

func TestMeterRecords(t *testing.T) {
	ctx := context.Background()
	m, registry := newTestMeter(t)
	opts := []gotel.InstrumentOption{gotel.WithAttributeKeys("http.method", "http.route")}
	counter, err := m.Counter("http.requests", opts...)
	if err != nil {
		t.Fatal(err)
	}
	counter.Increment(ctx, gotel.KV("http.method", "GET"), gotel.KV("undeclared", "ignored"))
	counter.Add(ctx, 2, gotel.KV("http.method", "POST"), gotel.KV("http.method", "GET"))
	active, err := m.UpDownCounter("http.active", opts...)
	if err != nil {
		t.Fatal(err)
	}
	active.Add(ctx, 2)
	active.Add(ctx, -1)
	histogram, err := m.Histogram("http.duration", gotel.WithUnit("s"), gotel.WithDescription("duration"), gotel.WithHistogramBoundaries(0.1, 1))
	if err != nil {
		t.Fatal(err)
	}
	histogram.Record(ctx, 0.5)
	gauge, err := m.Gauge("http.body", gotel.WithUnit("By"))
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
	metrics := gather(t, registry)
	requests := metrics["http_requests_total"].GetMetric()
	if len(requests) != 1 || requests[0].GetCounter().GetValue() != 3 {
		t.Fatalf("counter = %v", requests)
	}
	labels := requests[0].GetLabel()
	if len(labels) != 2 || labels[0].GetName() != "http_method" || labels[0].GetValue() != "GET" || labels[1].GetName() != "http_route" || labels[1].GetValue() != "" {
		t.Fatalf("labels = %v", labels)
	}
	if got := metrics["http_active"].GetMetric()[0].GetGauge().GetValue(); got != 1 {
		t.Fatalf("active = %v", got)
	}
	hist := metrics["http_duration_seconds"].GetMetric()[0].GetHistogram()
	if hist.GetSampleCount() != 1 || hist.GetSampleSum() != 0.5 || len(hist.GetBucket()) != 2 || hist.GetBucket()[1].GetUpperBound() != 1 {
		t.Fatalf("histogram = %v", hist)
	}
	if metrics["http_duration_seconds"].GetHelp() != "duration" {
		t.Fatalf("help = %v", metrics["http_duration_seconds"])
	}
	if got := metrics["http_body_bytes"].GetMetric()[0].GetGauge().GetValue(); got != 21 {
		t.Fatalf("gauge = %v", got)
	}
}

func TestMeterRegistration(t *testing.T) {
	m, registry := newTestMeter(t)
	first, err := m.Counter("requests")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Counter("requests")
	if err != nil {
		t.Fatal(err)
	}
	first.Increment(context.Background())
	second.Increment(context.Background())
	if got := gather(t, registry)["requests_total"].GetMetric()[0].GetCounter().GetValue(); got != 2 {
		t.Fatalf("reused counter = %v", got)
	}
	if _, err := m.Counter("requests", gotel.WithDescription("changed")); !errors.Is(err, ErrInstrumentConflict) {
		t.Fatalf("description conflict = %v", err)
	}
	if _, err := m.Gauge("requests_total"); !errors.Is(err, ErrInstrumentConflict) {
		t.Fatalf("kind conflict = %v", err)
	}
	if _, err := m.Gauge("http.route"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Gauge("http_route"); !errors.Is(err, ErrInstrumentConflict) {
		t.Fatalf("name conflict = %v", err)
	}
	if _, err := NewMeter(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil registry = %v", err)
	}
	external := prom.NewGauge(prom.GaugeOpts{Name: "external", Help: "external"})
	if err := registry.Register(external); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Gauge("external"); err == nil {
		t.Fatal("expected external registration conflict")
	}
}

func TestMeterInvalidConfig(t *testing.T) {
	m, _ := newTestMeter(t)
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
	}
	for _, keys := range [][]string{{"http.route", "http_route"}, {"__reserved"}, {"le"}} {
		if _, err := m.Histogram("invalid", gotel.WithAttributeKeys(keys...)); !errors.Is(err, gotel.ErrInvalidInstrument) {
			t.Fatalf("labels %v error = %v", keys, err)
		}
	}
}

func TestNames(t *testing.T) {
	for _, tt := range []struct{ input, want string }{{"http.route", "http_route"}, {"9name", "_9name"}, {"UPPER_case", "UPPER_case"}, {"a/中文", "a___"}} {
		if got := normalizeName(tt.input); got != tt.want {
			t.Fatalf("normalizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
	for _, tt := range []struct {
		name, unit string
		kind       instrumentKind
		want       string
	}{
		{"requests_total", "", counterKind, "requests_total"},
		{"duration_seconds", "s", histogramKind, "duration_seconds"},
		{"body_bytes", "By", gaugeKind, "body_bytes"},
		{"active", "{request}", upDownKind, "active"},
	} {
		if got := metricName(gotel.InstrumentConfig{Name: tt.name, Unit: tt.unit}, tt.kind); got != tt.want {
			t.Fatalf("metricName = %q, want %q", got, tt.want)
		}
	}
}

func TestMeterConcurrentCreationAndRecording(t *testing.T) {
	m, registry := newTestMeter(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			counter, err := m.Counter("requests")
			if err != nil {
				t.Error(err)
				return
			}
			for range 100 {
				counter.Increment(context.Background())
			}
		})
	}
	wg.Wait()
	if got := gather(t, registry)["requests_total"].GetMetric()[0].GetCounter().GetValue(); got != 1600 {
		t.Fatalf("counter = %v", got)
	}
}

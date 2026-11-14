// Package prometheus adapts a caller-owned Prometheus registry to gotel.Meter.
// It is a separate Go module and does not configure a global registry or server.
package prometheus

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/sofiworker/gk/gotel"
)

var (
	ErrInvalidConfig      = errors.New("gotel/prometheus: invalid configuration")
	ErrInstrumentConflict = errors.New("gotel/prometheus: conflicting instrument")
)

type Meter struct {
	registerer  prom.Registerer
	mu          sync.Mutex
	instruments map[string]*instrument
}

var _ gotel.Meter = (*Meter)(nil)

func NewMeter(registerer prom.Registerer) (*Meter, error) {
	if registerer == nil {
		return nil, fmt.Errorf("%w: nil registerer", ErrInvalidConfig)
	}
	return &Meter{registerer: registerer, instruments: make(map[string]*instrument)}, nil
}

func (m *Meter) Counter(name string, opts ...gotel.InstrumentOption) (gotel.Counter, error) {
	i, err := m.instrument(name, counterKind, opts...)
	if err != nil {
		return nil, err
	}
	return &counter{instrument: i}, nil
}

func (m *Meter) UpDownCounter(name string, opts ...gotel.InstrumentOption) (gotel.UpDownCounter, error) {
	i, err := m.instrument(name, upDownKind, opts...)
	if err != nil {
		return nil, err
	}
	return &upDownCounter{instrument: i}, nil
}

func (m *Meter) Histogram(name string, opts ...gotel.InstrumentOption) (gotel.Histogram, error) {
	i, err := m.instrument(name, histogramKind, opts...)
	if err != nil {
		return nil, err
	}
	return &recorder{instrument: i}, nil
}

func (m *Meter) Gauge(name string, opts ...gotel.InstrumentOption) (gotel.Gauge, error) {
	i, err := m.instrument(name, gaugeKind, opts...)
	if err != nil {
		return nil, err
	}
	return &recorder{instrument: i}, nil
}

type instrumentKind int

const (
	counterKind instrumentKind = iota
	upDownKind
	histogramKind
	gaugeKind
)

func (m *Meter) instrument(name string, kind instrumentKind, opts ...gotel.InstrumentOption) (*instrument, error) {
	c, err := gotel.NewInstrumentConfig(name, opts...)
	if err != nil {
		return nil, err
	}
	labels := make([]string, len(c.AttributeKeys))
	seen := make(map[string]bool, len(labels))
	for i, key := range c.AttributeKeys {
		label := normalizeName(key)
		if strings.HasPrefix(label, "__") || seen[label] || (kind == histogramKind && label == "le") {
			return nil, fmt.Errorf("%w: conflicting or reserved label %q", gotel.ErrInvalidInstrument, key)
		}
		labels[i], seen[label] = label, true
	}
	promName := metricName(c, kind)
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.instruments[promName]; ok {
		old := existing.config
		if existing.kind != kind || old.Name != c.Name || old.Unit != c.Unit || old.Description != c.Description ||
			!slices.Equal(old.AttributeKeys, c.AttributeKeys) || !slices.Equal(old.Boundaries, c.Boundaries) {
			return nil, fmt.Errorf("%w: %q maps to %q", ErrInstrumentConflict, name, promName)
		}
		return existing, nil
	}
	help := c.Description
	if help == "" {
		help = c.Name
	}
	i := &instrument{config: c, kind: kind}
	var collector prom.Collector
	switch kind {
	case counterKind:
		vec := prom.NewCounterVec(prom.CounterOpts{Name: promName, Help: help}, labels)
		collector = vec
		i.add = func(value float64, values []string) { vec.WithLabelValues(values...).Add(value) }
	case upDownKind, gaugeKind:
		vec := prom.NewGaugeVec(prom.GaugeOpts{Name: promName, Help: help}, labels)
		collector = vec
		i.add = func(value float64, values []string) { vec.WithLabelValues(values...).Add(value) }
		i.record = func(value float64, values []string) { vec.WithLabelValues(values...).Set(value) }
	case histogramKind:
		vec := prom.NewHistogramVec(prom.HistogramOpts{Name: promName, Help: help, Buckets: c.Boundaries}, labels)
		collector = vec
		i.record = func(value float64, values []string) { vec.WithLabelValues(values...).Observe(value) }
	}
	if err := m.registerer.Register(collector); err != nil {
		return nil, fmt.Errorf("gotel/prometheus: register %q: %w", promName, err)
	}
	m.instruments[promName] = i
	return i, nil
}

func normalizeName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		} else {
			result.WriteByte('_')
		}
	}
	normalized := result.String()
	if normalized != "" && normalized[0] >= '0' && normalized[0] <= '9' {
		return "_" + normalized
	}
	return normalized
}

func metricName(c gotel.InstrumentConfig, kind instrumentKind) string {
	name := normalizeName(c.Name)
	suffix := ""
	switch c.Unit {
	case "s":
		suffix = "_seconds"
	case "By":
		suffix = "_bytes"
	}
	if suffix != "" && !strings.HasSuffix(name, suffix) {
		name += suffix
	}
	if kind == counterKind && !strings.HasSuffix(name, "_total") {
		name += "_total"
	}
	return name
}

type instrument struct {
	config gotel.InstrumentConfig
	kind   instrumentKind
	add    func(float64, []string)
	record func(float64, []string)
}

func (i *instrument) labelValues(attrs []gotel.KeyValue) []string {
	values := i.config.Attributes(attrs...)
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = fmt.Sprint(value.Value)
	}
	return result
}

type counter struct{ instrument *instrument }
type upDownCounter struct{ instrument *instrument }
type recorder struct{ instrument *instrument }

func (i *instrument) addValue(value float64, attrs []gotel.KeyValue) {
	if math.IsNaN(value) || math.IsInf(value, 0) || (i.kind == counterKind && value < 0) {
		return
	}
	i.add(value, i.labelValues(attrs))
}

func (c *counter) Add(_ context.Context, value float64, attrs ...gotel.KeyValue) {
	c.instrument.addValue(value, attrs)
}
func (c *counter) Increment(ctx context.Context, attrs ...gotel.KeyValue) { c.Add(ctx, 1, attrs...) }
func (c *upDownCounter) Add(_ context.Context, value float64, attrs ...gotel.KeyValue) {
	c.instrument.addValue(value, attrs)
}

func (r *recorder) Record(_ context.Context, value float64, attrs ...gotel.KeyValue) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	i := r.instrument
	i.record(value, i.labelValues(attrs))
}

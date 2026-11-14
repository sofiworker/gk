// Package otel adapts OpenTelemetry APIs to gotel in a separate Go module.
// NewTracer/NewMeter borrow caller-owned APIs; NewProvider owns its SDK
// providers, configured exporter and readers, including flush and shutdown.
package otel

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/sofiworker/gk/gotel"
	"go.opentelemetry.io/otel/metric"
)

var ErrInvalidConfig = errors.New("gotel/otel: invalid configuration")

type Meter struct{ meter metric.Meter }

var _ gotel.Meter = (*Meter)(nil)

func NewMeter(meter metric.Meter) (*Meter, error) {
	if meter == nil {
		return nil, fmt.Errorf("%w: nil meter", ErrInvalidConfig)
	}
	return &Meter{meter: meter}, nil
}

func (m *Meter) Counter(name string, opts ...gotel.InstrumentOption) (gotel.Counter, error) {
	c, err := gotel.NewInstrumentConfig(name, opts...)
	if err != nil {
		return nil, err
	}
	i, err := m.meter.Float64Counter(c.Name, metric.WithDescription(c.Description), metric.WithUnit(c.Unit))
	if err != nil {
		return nil, fmt.Errorf("gotel/otel: create counter %q: %w", name, err)
	}
	return &addInstrument{add: i.Add, config: c}, nil
}

func (m *Meter) UpDownCounter(name string, opts ...gotel.InstrumentOption) (gotel.UpDownCounter, error) {
	c, err := gotel.NewInstrumentConfig(name, opts...)
	if err != nil {
		return nil, err
	}
	i, err := m.meter.Float64UpDownCounter(c.Name, metric.WithDescription(c.Description), metric.WithUnit(c.Unit))
	if err != nil {
		return nil, fmt.Errorf("gotel/otel: create up-down counter %q: %w", name, err)
	}
	return &addInstrument{add: i.Add, config: c, signed: true}, nil
}

func (m *Meter) Histogram(name string, opts ...gotel.InstrumentOption) (gotel.Histogram, error) {
	c, err := gotel.NewInstrumentConfig(name, opts...)
	if err != nil {
		return nil, err
	}
	instrumentOpts := []metric.Float64HistogramOption{metric.WithDescription(c.Description), metric.WithUnit(c.Unit)}
	if len(c.Boundaries) > 0 {
		instrumentOpts = append(instrumentOpts, metric.WithExplicitBucketBoundaries(c.Boundaries...))
	}
	i, err := m.meter.Float64Histogram(c.Name, instrumentOpts...)
	if err != nil {
		return nil, fmt.Errorf("gotel/otel: create histogram %q: %w", name, err)
	}
	return &recordInstrument{record: i.Record, config: c}, nil
}

func (m *Meter) Gauge(name string, opts ...gotel.InstrumentOption) (gotel.Gauge, error) {
	c, err := gotel.NewInstrumentConfig(name, opts...)
	if err != nil {
		return nil, err
	}
	i, err := m.meter.Float64Gauge(c.Name, metric.WithDescription(c.Description), metric.WithUnit(c.Unit))
	if err != nil {
		return nil, fmt.Errorf("gotel/otel: create gauge %q: %w", name, err)
	}
	return &recordInstrument{record: i.Record, config: c}, nil
}

type addInstrument struct {
	add    func(context.Context, float64, ...metric.AddOption)
	config gotel.InstrumentConfig
	signed bool
}

func (i *addInstrument) Add(ctx context.Context, value float64, attrs ...gotel.KeyValue) {
	if math.IsNaN(value) || math.IsInf(value, 0) || (!i.signed && value < 0) {
		return
	}
	i.add(ctx, value, metric.WithAttributes(attributes(i.config.Attributes(attrs...))...))
}

func (i *addInstrument) Increment(ctx context.Context, attrs ...gotel.KeyValue) {
	i.Add(ctx, 1, attrs...)
}

type recordInstrument struct {
	record func(context.Context, float64, ...metric.RecordOption)
	config gotel.InstrumentConfig
}

func (i *recordInstrument) Record(ctx context.Context, value float64, attrs ...gotel.KeyValue) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	i.record(ctx, value, metric.WithAttributes(attributes(i.config.Attributes(attrs...))...))
}

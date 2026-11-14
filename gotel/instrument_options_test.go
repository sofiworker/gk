package gotel

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestInstrumentConfig(t *testing.T) {
	c, err := NewInstrumentConfig("request.duration", nil, WithDescription("duration"),
		WithUnit("s"), WithAttributeKeys("method", "route"), WithHistogramBoundaries(0, 0.1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "request.duration" || c.Description != "duration" || c.Unit != "s" {
		t.Fatalf("config = %+v", c)
	}
	want := []KeyValue{KV("method", "POST"), KV("route", "")}
	if got := c.Attributes(KV("extra", 1), KV("method", "GET"), KV("method", "POST")); !reflect.DeepEqual(got, want) {
		t.Fatalf("attributes = %#v, want %#v", got, want)
	}
	if got := (InstrumentConfig{}).Attributes(KV("ignored", true)); len(got) != 0 {
		t.Fatalf("undeclared attributes = %v", got)
	}
}

func TestInvalidInstrumentConfig(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		opts   []InstrumentOption
	}{
		{"empty name", " ", nil},
		{"empty key", "metric", []InstrumentOption{WithAttributeKeys(" ")}},
		{"duplicate key", "metric", []InstrumentOption{WithAttributeKeys("route", "route")}},
		{"duplicate boundary", "metric", []InstrumentOption{WithHistogramBoundaries(1, 1)}},
		{"descending boundary", "metric", []InstrumentOption{WithHistogramBoundaries(2, 1)}},
		{"nan", "metric", []InstrumentOption{WithHistogramBoundaries(math.NaN())}},
		{"inf", "metric", []InstrumentOption{WithHistogramBoundaries(math.Inf(1))}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewInstrumentConfig(tt.metric, tt.opts...); !errors.Is(err, ErrInvalidInstrument) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestInstrumentOptionsOwnSlices(t *testing.T) {
	keys, bounds := []string{"route"}, []float64{1, 2}
	opts := []InstrumentOption{WithAttributeKeys(keys...), WithHistogramBoundaries(bounds...)}
	keys[0], bounds[0] = "changed", 99
	first, err := NewInstrumentConfig("metric", opts...)
	if err != nil {
		t.Fatal(err)
	}
	first.AttributeKeys[0], first.Boundaries[0] = "mutated", 88
	second, err := NewInstrumentConfig("metric", opts...)
	if err != nil {
		t.Fatal(err)
	}
	if second.AttributeKeys[0] != "route" || second.Boundaries[0] != 1 {
		t.Fatalf("shared slices: %+v", second)
	}
}

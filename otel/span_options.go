package gotel

import (
	"slices"
	"time"
)

type SpanStartConfig struct {
	Kind       SpanKind
	Attributes []KeyValue
	Timestamp  time.Time
}

type SpanStartOption func(*SpanStartConfig)

func NewSpanStartConfig(opts ...SpanStartOption) SpanStartConfig {
	var c SpanStartConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	c.Attributes = slices.Clone(c.Attributes)
	return c
}

func WithSpanKind(kind SpanKind) SpanStartOption {
	return func(c *SpanStartConfig) { c.Kind = kind }
}

func WithSpanAttributes(attributes ...KeyValue) SpanStartOption {
	attributes = slices.Clone(attributes)
	return func(c *SpanStartConfig) { c.Attributes = append(c.Attributes, attributes...) }
}

func WithSpanStartTime(timestamp time.Time) SpanStartOption {
	return func(c *SpanStartConfig) { c.Timestamp = timestamp }
}

type SpanEndConfig struct {
	Timestamp time.Time
}

type SpanEndOption func(*SpanEndConfig)

func NewSpanEndConfig(opts ...SpanEndOption) SpanEndConfig {
	var c SpanEndConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	return c
}

func WithSpanEndTime(timestamp time.Time) SpanEndOption {
	return func(c *SpanEndConfig) { c.Timestamp = timestamp }
}

package gotel

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

// ErrInvalidInstrument indicates invalid instrument configuration.
var ErrInvalidInstrument = errors.New("gotel: invalid instrument")

// InstrumentConfig is shared by adapters. AttributeKeys declares the complete
// attribute schema: undeclared attributes are ignored; missing values are "".
type InstrumentConfig struct {
	Name          string
	Description   string
	Unit          string
	AttributeKeys []string
	Boundaries    []float64
}

type InstrumentOption func(*InstrumentConfig)

// NewInstrumentConfig resolves options and validates portable instrument settings.
// Boundaries, when set, must be finite and strictly increasing.
func NewInstrumentConfig(name string, opts ...InstrumentOption) (InstrumentConfig, error) {
	c := InstrumentConfig{Name: name}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if strings.TrimSpace(c.Name) == "" {
		return InstrumentConfig{}, fmt.Errorf("%w: empty name", ErrInvalidInstrument)
	}
	keys := make(map[string]struct{}, len(c.AttributeKeys))
	for _, key := range c.AttributeKeys {
		if strings.TrimSpace(key) == "" {
			return InstrumentConfig{}, fmt.Errorf("%w: empty attribute key", ErrInvalidInstrument)
		}
		if _, exists := keys[key]; exists {
			return InstrumentConfig{}, fmt.Errorf("%w: duplicate attribute key %q", ErrInvalidInstrument, key)
		}
		keys[key] = struct{}{}
	}
	for i, bound := range c.Boundaries {
		if math.IsNaN(bound) || math.IsInf(bound, 0) || (i > 0 && bound <= c.Boundaries[i-1]) {
			return InstrumentConfig{}, fmt.Errorf("%w: boundaries must be finite and strictly increasing", ErrInvalidInstrument)
		}
	}
	c.AttributeKeys = slices.Clone(c.AttributeKeys)
	c.Boundaries = slices.Clone(c.Boundaries)
	return c, nil
}

func WithDescription(description string) InstrumentOption {
	return func(c *InstrumentConfig) { c.Description = description }
}

func WithUnit(unit string) InstrumentOption {
	return func(c *InstrumentConfig) { c.Unit = unit }
}

func WithAttributeKeys(keys ...string) InstrumentOption {
	keys = slices.Clone(keys)
	return func(c *InstrumentConfig) { c.AttributeKeys = slices.Clone(keys) }
}

func WithHistogramBoundaries(boundaries ...float64) InstrumentOption {
	boundaries = slices.Clone(boundaries)
	return func(c *InstrumentConfig) { c.Boundaries = slices.Clone(boundaries) }
}

// Attributes returns values in schema order. The last duplicate value wins.
func (c InstrumentConfig) Attributes(attributes ...KeyValue) []KeyValue {
	result := make([]KeyValue, len(c.AttributeKeys))
	for i, key := range c.AttributeKeys {
		result[i] = KV(key, "")
		for _, attribute := range attributes {
			if attribute.Key == key {
				result[i] = attribute
			}
		}
	}
	return result
}

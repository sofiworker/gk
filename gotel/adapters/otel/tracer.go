package otel

import (
	"context"
	"errors"
	"fmt"

	"github.com/sofiworker/gk/gotel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var ErrInvalidSpanContext = errors.New("gotel/otel: invalid span context")

type Tracer struct {
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

type tracerConfig struct{ propagator propagation.TextMapPropagator }
type TracerOption func(*tracerConfig)

var _ gotel.Tracer = (*Tracer)(nil)

func WithPropagator(propagator propagation.TextMapPropagator) TracerOption {
	return func(c *tracerConfig) { c.propagator = propagator }
}

func NewTracer(tracer trace.Tracer, opts ...TracerOption) (*Tracer, error) {
	c := tracerConfig{propagator: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if tracer == nil || c.propagator == nil {
		return nil, fmt.Errorf("%w: nil tracer or propagator", ErrInvalidConfig)
	}
	return &Tracer{tracer: tracer, propagator: c.propagator}, nil
}

func (t *Tracer) Start(ctx context.Context, name string, opts ...gotel.SpanStartOption) (context.Context, gotel.Span) {
	c := gotel.NewSpanStartConfig(opts...)
	kind := trace.SpanKindInternal
	if c.Kind >= gotel.SpanKindInternal && c.Kind <= gotel.SpanKindConsumer {
		kind = trace.SpanKind(c.Kind + 1)
	}
	spanOpts := []trace.SpanStartOption{trace.WithSpanKind(kind), trace.WithAttributes(attributes(c.Attributes)...)}
	if !c.Timestamp.IsZero() {
		spanOpts = append(spanOpts, trace.WithTimestamp(c.Timestamp))
	}
	ctx, s := t.tracer.Start(ctx, name, spanOpts...)
	return ctx, &span{span: s}
}

func (t *Tracer) Extract(ctx context.Context, carrier gotel.TextMapCarrier) (context.Context, gotel.SpanContext) {
	if carrier != nil {
		ctx = t.propagator.Extract(ctx, carrier)
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ctx, nil
	}
	return ctx, spanContext{SpanContext: sc}
}

func (t *Tracer) Inject(ctx context.Context, carrier gotel.TextMapCarrier, sc gotel.SpanContext) error {
	if carrier == nil {
		return fmt.Errorf("%w: nil carrier", ErrInvalidConfig)
	}
	if sc != nil {
		var native trace.SpanContext
		if wrapped, ok := sc.(spanContext); ok {
			native = wrapped.SpanContext
		} else {
			traceID, err := trace.TraceIDFromHex(sc.TraceID())
			if err != nil {
				return fmt.Errorf("%w: trace ID: %v", ErrInvalidSpanContext, err)
			}
			spanID, err := trace.SpanIDFromHex(sc.SpanID())
			if err != nil {
				return fmt.Errorf("%w: span ID: %v", ErrInvalidSpanContext, err)
			}
			var flags trace.TraceFlags
			if sc.IsSampled() {
				flags = trace.FlagsSampled
			}
			native = trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: flags})
		}
		if !native.IsValid() {
			return ErrInvalidSpanContext
		}
		ctx = trace.ContextWithSpanContext(ctx, native)
	}
	t.propagator.Inject(ctx, carrier)
	return nil
}

type span struct{ span trace.Span }

func (s *span) Context() gotel.SpanContext            { return spanContext{SpanContext: s.span.SpanContext()} }
func (s *span) SetAttributes(attrs ...gotel.KeyValue) { s.span.SetAttributes(attributes(attrs)...) }
func (s *span) SetStatus(code gotel.StatusCode, description string) {
	status := codes.Unset
	switch code {
	case gotel.StatusCodeOk:
		status = codes.Ok
	case gotel.StatusCodeError:
		status = codes.Error
	}
	s.span.SetStatus(status, description)
}
func (s *span) RecordError(err error, attrs ...gotel.KeyValue) {
	s.span.RecordError(err, trace.WithAttributes(attributes(attrs)...))
}
func (s *span) AddEvent(name string, attrs ...gotel.KeyValue) {
	s.span.AddEvent(name, trace.WithAttributes(attributes(attrs)...))
}
func (s *span) End(opts ...gotel.SpanEndOption) {
	c := gotel.NewSpanEndConfig(opts...)
	if c.Timestamp.IsZero() {
		s.span.End()
		return
	}
	s.span.End(trace.WithTimestamp(c.Timestamp))
}

type spanContext struct{ trace.SpanContext }

func (s spanContext) TraceID() string { return s.SpanContext.TraceID().String() }
func (s spanContext) SpanID() string  { return s.SpanContext.SpanID().String() }
func (s spanContext) Serialize() map[string]string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(trace.ContextWithSpanContext(context.Background(), s.SpanContext), carrier)
	return map[string]string(carrier)
}

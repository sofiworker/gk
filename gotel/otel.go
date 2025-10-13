package gotel

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type OTELProvider struct {
	tracer     trace.Tracer
	meter      metric.Meter
	propagator propagation.TextMapPropagator
}

//func NewOTELProvider(serviceName string) Provider {
//	tp := sdktrace.NewTracerProvider()
//	mp := metric.NewMeterProvider()
//
//	return &OTELProvider{
//		tracer:     tp.Tracer(serviceName),
//		meter:      mp.Meter(serviceName),
//		propagator: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
//	}
//}
//
//func (p *OTELProvider) Start(ctx context.Context, spanName string, opts ...SpanStartOption) (context.Context, Span) {
//	ctx, span := p.tracer.Start(ctx, spanName)
//	return ctx, &OTELSpan{span: span}
//}
//
//func (p *OTELProvider) Extract(ctx context.Context, carrier TextMapCarrier) (context.Context, SpanContext) {
//	carrierImpl := &textMapCarrier{carrier: carrier}
//	ctx = p.propagator.Extract(ctx, carrierImpl)
//
//	if span := trace.SpanFromContext(ctx); span != nil {
//		return ctx, &OTELSpanContext{spanContext: span.SpanContext()}
//	}
//	return ctx, nil
//}
//
//func (p *OTELProvider) Inject(ctx context.Context, carrier TextMapCarrier, spanContext SpanContext) error {
//	carrierImpl := &textMapCarrier{carrier: carrier}
//
//	// 创建包含指定 SpanContext 的上下文
//	var sc trace.SpanContext
//	if otelSC, ok := spanContext.(*OTELSpanContext); ok {
//		sc = otelSC.spanContext
//	}
//
//	ctx = trace.ContextWithSpanContext(ctx, sc)
//	p.propagator.Inject(ctx, carrierImpl)
//	return nil
//}
//
//func (p *OTELProvider) Counter(name string, opts ...InstrumentOption) Counter {
//	counter, _ := p.meter.Float64Counter(name)
//	return &OTELCounter{counter: counter}
//}
//
//func (p *OTELProvider) Histogram(name string, opts ...InstrumentOption) Histogram {
//	histogram, _ := p.meter.Float64Histogram(name)
//	return &OTELHistogram{histogram: histogram}
//}
//
//func (p *OTELProvider) Gauge(name string, opts ...InstrumentOption) Gauge {
//	gauge, _ := p.meter.Float64ObservableGauge(name)
//	return &OTELGauge{gauge: gauge}
//}
//
//// OTELSpan 实现
//type OTELSpan struct {
//	span trace.Span
//}
//
//func (s *OTELSpan) Context() SpanContext {
//	return &OTELSpanContext{spanContext: s.span.SpanContext()}
//}
//
//func (s *OTELSpan) SetAttributes(attributes ...KeyValue) {
//	attrs := convertAttributes(attributes)
//	s.span.SetAttributes(attrs...)
//}
//
//func (s *OTELSpan) SetStatus(code StatusCode, description string) {
//	var otelCode otelcodes.Code
//	switch code {
//	case StatusCodeOk:
//		otelCode = otelcodes.Ok
//	case StatusCodeError:
//		otelCode = otelcodes.Error
//	default:
//		otelCode = otelcodes.Unset
//	}
//	s.span.SetStatus(otelCode, description)
//}
//
//func (s *OTELSpan) RecordError(err error, attributes ...KeyValue) {
//	attrs := convertAttributes(attributes)
//	s.span.RecordError(err, trace.WithAttributes(attrs...))
//}
//
//func (s *OTELSpan) AddEvent(name string, attributes ...KeyValue) {
//	attrs := convertAttributes(attributes)
//	s.span.AddEvent(name, trace.WithAttributes(attrs...))
//}
//
//func (s *OTELSpan) End(options ...SpanEndOption) {
//	s.span.End()
//}
//
//// OTELSpanContext 实现
//type OTELSpanContext struct {
//	spanContext trace.SpanContext
//}
//
//func (sc *OTELSpanContext) TraceID() string {
//	return sc.spanContext.TraceID().String()
//}
//
//func (sc *OTELSpanContext) SpanID() string {
//	return sc.spanContext.SpanID().String()
//}
//
//func (sc *OTELSpanContext) IsSampled() bool {
//	return sc.spanContext.IsSampled()
//}
//
//func (sc *OTELSpanContext) Serialize() map[string]string {
//	return map[string]string{
//		"traceparent": sc.spanContext.TraceID().String() + "-" + sc.spanContext.SpanID().String(),
//	}
//}

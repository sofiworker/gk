package otel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sofiworker/gk/gotel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestTracerPropagationAndSpan(t *testing.T) {
	ctx := context.Background()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	tracer, err := NewTracer(provider.Tracer("test"), nil)
	if err != nil {
		t.Fatal(err)
	}
	incoming := propagation.MapCarrier{"traceparent": "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01", "tracestate": "vendor=value"}
	ctx, parent := tracer.Extract(ctx, incoming)
	if parent == nil || parent.TraceID() != "0123456789abcdef0123456789abcdef" || parent.SpanID() != "0123456789abcdef" || !parent.IsSampled() {
		t.Fatalf("parent = %#v", parent)
	}
	start := time.Unix(10, 0)
	ctx, span := tracer.Start(ctx, "request", gotel.WithSpanKind(gotel.SpanKindServer), gotel.WithSpanStartTime(start), gotel.WithSpanAttributes(gotel.KV("initial", true)))
	span.SetAttributes(gotel.KV("route", "/users"))
	span.SetStatus(gotel.StatusCodeError, "failed")
	span.RecordError(errors.New("failure"), gotel.KV("retry", 1))
	span.AddEvent("event", gotel.KV("event.attr", "value"))
	outgoing := propagation.MapCarrier{}
	if err := tracer.Inject(ctx, outgoing, span.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outgoing["traceparent"], parent.TraceID()) || outgoing["tracestate"] != "vendor=value" {
		t.Fatalf("injected = %v", outgoing)
	}
	if serialized := span.Context().Serialize(); serialized["traceparent"] != outgoing["traceparent"] || serialized["tracestate"] != "vendor=value" {
		t.Fatalf("serialized = %v", serialized)
	}
	span.End(gotel.WithSpanEndTime(start.Add(time.Second)))
	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended = %d", len(ended))
	}
	s := ended[0]
	if s.Name() != "request" || s.SpanKind() != trace.SpanKindServer || s.Parent().SpanID().String() != parent.SpanID() || s.Status().Code != codes.Error || s.Status().Description != "failed" {
		t.Fatalf("span = %+v", s)
	}
	if s.StartTime() != start || s.EndTime() != start.Add(time.Second) || len(s.Attributes()) != 2 || len(s.Events()) != 2 {
		t.Fatalf("span times, attributes or events = %+v", s)
	}
	_, defaultSpan := tracer.Start(ctx, "default")
	defaultSpan.End()
	if len(recorder.Ended()) != 2 {
		t.Fatal("default End did not finish span")
	}
	carrier := propagation.MapCarrier{}
	if err := tracer.Inject(ctx, carrier, nil); err != nil || carrier["traceparent"] == "" {
		t.Fatalf("context injection = %v/%v", carrier, err)
	}
}

type foreignContext struct {
	traceID, spanID string
	sampled         bool
}

func (s foreignContext) TraceID() string              { return s.traceID }
func (s foreignContext) SpanID() string               { return s.spanID }
func (s foreignContext) IsSampled() bool              { return s.sampled }
func (s foreignContext) Serialize() map[string]string { return nil }

func TestTracerConfigAndForeignContext(t *testing.T) {
	native := noop.NewTracerProvider().Tracer("test")
	if _, err := NewTracer(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil tracer = %v", err)
	}
	if _, err := NewTracer(native, WithPropagator(nil)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil propagator = %v", err)
	}
	tracer, err := NewTracer(native)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, sc := tracer.Extract(ctx, nil); sc != nil {
		t.Fatalf("empty extraction = %v", sc)
	}
	if err := tracer.Inject(ctx, nil, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil carrier = %v", err)
	}
	for _, sampled := range []bool{false, true} {
		carrier := propagation.MapCarrier{}
		sc := foreignContext{"0123456789abcdef0123456789abcdef", "0123456789abcdef", sampled}
		if err := tracer.Inject(ctx, carrier, sc); err != nil {
			t.Fatal(err)
		}
		want := "-00"
		if sampled {
			want = "-01"
		}
		if !strings.HasSuffix(carrier["traceparent"], want) {
			t.Fatalf("sampling = %v", carrier)
		}
	}
	for _, sc := range []gotel.SpanContext{foreignContext{}, foreignContext{traceID: "0123456789abcdef0123456789abcdef"}, spanContext{}} {
		if err := tracer.Inject(ctx, propagation.MapCarrier{}, sc); !errors.Is(err, ErrInvalidSpanContext) {
			t.Fatalf("invalid context = %v", err)
		}
	}
	custom, err := NewTracer(native, WithPropagator(propagation.NewCompositeTextMapPropagator()))
	if err != nil {
		t.Fatal(err)
	}
	carrier := propagation.MapCarrier{}
	if err := custom.Inject(ctx, carrier, foreignContext{"0123456789abcdef0123456789abcdef", "0123456789abcdef", true}); err != nil || len(carrier) != 0 {
		t.Fatalf("custom propagation = %v/%v", carrier, err)
	}
}

func TestSpanKindsAndStatuses(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	tracer, err := NewTracer(provider.Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		kind       gotel.SpanKind
		status     gotel.StatusCode
		wantKind   trace.SpanKind
		wantStatus codes.Code
	}{
		{gotel.SpanKindInternal, gotel.StatusCodeUnset, trace.SpanKindInternal, codes.Unset},
		{gotel.SpanKindClient, gotel.StatusCodeOk, trace.SpanKindClient, codes.Ok},
		{gotel.SpanKindProducer, gotel.StatusCodeError, trace.SpanKindProducer, codes.Error},
		{gotel.SpanKindConsumer, gotel.StatusCodeUnset, trace.SpanKindConsumer, codes.Unset},
		{gotel.SpanKind(-1), gotel.StatusCode(-1), trace.SpanKindInternal, codes.Unset},
	} {
		_, span := tracer.Start(context.Background(), "kind", gotel.WithSpanKind(tt.kind))
		span.SetStatus(tt.status, "status")
		span.End()
		s := recorder.Ended()[len(recorder.Ended())-1]
		if s.SpanKind() != tt.wantKind || s.Status().Code != tt.wantStatus {
			t.Fatalf("kind/status = %v/%v", s.SpanKind(), s.Status())
		}
	}
}

package gclient

import (
	"context"
	"testing"
)

func TestOpenTelemetryTracer_StartSpan(t *testing.T) {
	tracer := &OpenTelemetryTracer{}

	ctx, finish := tracer.StartSpan(context.Background())
	if ctx == nil {
		t.Error("Expected context to be created")
	}

	if finish == nil {
		t.Error("Expected finish function to be created")
	}

	// Test with nil context
	ctx, finish = tracer.StartSpan(nil)
	if ctx == nil {
		t.Error("Expected context to be created even with nil input")
	}

	if finish == nil {
		t.Error("Expected finish function to be created even with nil input")
	}
	finish()
}

func TestOpenTelemetryTracer_SetAttribute(t *testing.T) {
	tracer := &OpenTelemetryTracer{}

	// Test setting attributes without span (should not panic)
	tracer.SetAttribute("key", "value")

	// Test setting various types of attributes (mock span would be needed for full testing)
	tracer.SetAttribute("string_key", "string_value")
	tracer.SetAttribute("int_key", 42)
	tracer.SetAttribute("int64_key", int64(42))
	tracer.SetAttribute("float64_key", float64(3.14))
	tracer.SetAttribute("bool_key", true)

	// Test default case
	tracer.SetAttribute("default_key", "default_value")
}

func TestOpenTelemetryTracer_SpanName(t *testing.T) {
	tracer := &OpenTelemetryTracer{}
	expected := "ghttp_request"

	if name := tracer.SpanName(); name != expected {
		t.Errorf("Expected span name %s, got %s", expected, name)
	}
}

func TestJaegerTracer(t *testing.T) {
	tracer := &JaegerTracer{}

	ctx, finish := tracer.StartSpan(context.Background())
	if ctx == nil {
		t.Error("Expected context to be created")
	}

	if finish == nil {
		t.Error("Expected finish function to be created")
	}

	// Test methods don't panic
	tracer.SetAttribute("key", "value")

	expected := "ghttp_request"
	if name := tracer.SpanName(); name != expected {
		t.Errorf("Expected span name %s, got %s", expected, name)
	}

	finish()
}

func TestNoopTracer(t *testing.T) {
	tracer := &NoopTracer{}

	ctx, finish := tracer.StartSpan(context.Background())
	if ctx == nil {
		t.Error("Expected context to be created")
	}

	if finish == nil {
		t.Error("Expected finish function to be created")
	}

	// Test methods don't panic
	tracer.SetAttribute("key", "value")

	expected := "ghttp_request"
	if name := tracer.SpanName(); name != expected {
		t.Errorf("Expected span name %s, got %s", expected, name)
	}

	finish()
}

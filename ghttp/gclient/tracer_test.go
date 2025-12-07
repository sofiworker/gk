package gclient

import (
	"context"
	"testing"
)

func TestNoopTracer(t *testing.T) {
	nt := &NoopTracer{}
	if nt.SpanName() == "" { t.Error("empty span name") }
	nt.SetAttribute("k", "v")
	ctx, end := nt.StartSpan(context.Background())
	if ctx == nil { t.Error("nil ctx") }
	end()
}

func TestOpenTelemetryTracer(t *testing.T) {
	// We can't easily test side effects without a mock Provider, 
	// but we can ensure methods don't panic.
	ott := &OpenTelemetryTracer{}
	if ott.SpanName() == "" { t.Error("empty span name") }
	
	// StartSpan (will use global Noop provider if not configured)
	_, end := ott.StartSpan(context.Background())
	ott.SetAttribute("key", "value")
	ott.SetAttribute("int", 1)
	ott.SetAttribute("bool", true)
	end()
}
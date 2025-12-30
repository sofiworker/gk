package glog_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sofiworker/gk/glog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
)

type recordingSpan struct {
	embedded.Span
	sc trace.SpanContext
}

func (recordingSpan) span()                                               {}
func (recordingSpan) End(options ...trace.SpanEndOption)                  {}
func (recordingSpan) AddEvent(name string, options ...trace.EventOption)  {}
func (recordingSpan) AddLink(link trace.Link)                             {}
func (recordingSpan) IsRecording() bool                                   { return true }
func (recordingSpan) RecordError(err error, options ...trace.EventOption) {}
func (s recordingSpan) SpanContext() trace.SpanContext                    { return s.sc }
func (recordingSpan) SetStatus(code codes.Code, description string)       {}
func (recordingSpan) SetName(name string)                                 {}
func (recordingSpan) SetAttributes(kv ...attribute.KeyValue)              {}
func (recordingSpan) TracerProvider() trace.TracerProvider                { return nil }

func TestContextLoggingTraceFields(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)
	err := glog.Configure(
		glog.WithOutputPaths(logFilePath),
		glog.WithEncoding(glog.JSONEncoding),
		glog.WithDisableCaller(true),
	)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("Failed to build trace ID: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("Failed to build span ID: %v", err)
	}

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	ctx := trace.ContextWithSpan(context.Background(), recordingSpan{sc: spanCtx})

	glog.InfoContext(ctx, "with trace")
	time.Sleep(100 * time.Millisecond)

	content, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(content))), &data); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}
	if data["trace_id"] != traceID.String() {
		t.Fatalf("expected trace_id %q, got %v", traceID.String(), data["trace_id"])
	}
	if data["span_id"] != spanID.String() {
		t.Fatalf("expected span_id %q, got %v", spanID.String(), data["span_id"])
	}
}

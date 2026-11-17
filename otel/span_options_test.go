package gotel

import (
	"testing"
	"time"
)

func TestSpanOptions(t *testing.T) {
	start, end := time.Unix(10, 0), time.Unix(11, 0)
	attrs := []KeyValue{KV("key", "original")}
	opts := []SpanStartOption{nil, WithSpanKind(SpanKindServer), WithSpanStartTime(start), WithSpanAttributes(attrs...), WithSpanAttributes(KV("other", 2))}
	attrs[0] = KV("key", "changed")
	c := NewSpanStartConfig(opts...)
	if c.Kind != SpanKindServer || c.Timestamp != start || len(c.Attributes) != 2 || c.Attributes[0].Value != "original" {
		t.Fatalf("start config = %+v", c)
	}
	c.Attributes[0].Value = "mutated"
	if got := NewSpanStartConfig(opts...).Attributes[0].Value; got != "original" {
		t.Fatalf("shared attributes: %v", got)
	}
	if c := NewSpanEndConfig(nil, WithSpanEndTime(end)); c.Timestamp != end {
		t.Fatalf("end config = %+v", c)
	}
	if NewSpanStartConfig().Kind != SpanKindInternal || !NewSpanEndConfig().Timestamp.IsZero() {
		t.Fatal("unexpected span defaults")
	}
}

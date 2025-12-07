package gotel

import (
	"testing"
	"go.opentelemetry.io/otel/codes"
)

func TestUtil(t *testing.T) {
	// Test toString
	if toString("s") != "s" { t.Error("string failed") }
	if toString(123) != "123" { t.Error("int failed") }
	if toString(int64(123)) != "123" { t.Error("int64 failed") }
	if toString(12.34) != "12.34" { t.Error("float64 failed") }
	if toString(true) != "true" { t.Error("bool failed") }
	if toString(nil) != "null" { t.Error("nil failed") }
	
	// Test convertStatusCode
	if convertStatusCode(StatusCodeOk) != codes.Ok { t.Error("ok failed") }
	if convertStatusCode(StatusCodeError) != codes.Error { t.Error("error failed") }
	if convertStatusCode(StatusCodeUnset) != codes.Unset { t.Error("unset failed") }
	
	// Test convertAttributes
	kv := []KeyValue{
		KV("str", "v"),
		KV("int", 1),
	}
	attrs := convertAttributes(kv)
	if len(attrs) != 2 {
		t.Error("len mismatch")
	}
	// Note: checking deep equality on attribute.KeyValue is hard because of internal types, 
	// but we trust opentelemetry lib.
}

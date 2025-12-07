package gcodec

import (
	"bytes"
	"testing"
)

func TestHTTPCodec(t *testing.T) {
	hc := NewHTTPCodec()

	// Test Default Registration
	if _, ok := hc.GetCodec("application/json"); !ok {
		t.Error("application/json should be registered")
	}
	if _, ok := hc.GetCodec("text/plain"); !ok {
		t.Error("text/plain should be registered")
	}

	// Test Register and Get
	mockCodec := NewPlainCodec()
	hc.RegisterCodec("application/custom", mockCodec)
	
	if c, ok := hc.GetCodec("application/custom"); !ok || c != mockCodec {
		t.Error("application/custom retrieval failed")
	}

	// Test Normalize
	if _, ok := hc.GetCodec("APPLICATION/JSON; charset=utf-8"); !ok {
		t.Error("normalization failed")
	}

	// Test Encode (defaults to JSON if not specified logic, but Encode/Decode impl 
	// checks application/json or first codec. 
	// Wait, the implementation of Encode hardcodes checking "application/json" first.
	
	type Data struct {
		Val string `json:"val"`
	}
	d := Data{Val: "test"}
	
	var buf bytes.Buffer
	if err := hc.Encode(&buf, d); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"val":"test"`)) {
		t.Errorf("expected json output, got %s", buf.String())
	}

	// Test Decode
	buf.Reset()
	buf.WriteString(`{"val":"decoded"}`)
	var out Data
	if err := hc.Decode(&buf, &out); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if out.Val != "decoded" {
		t.Errorf("expected 'decoded', got %s", out.Val)
	}
}

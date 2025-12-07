package gcodec

import (
	"bytes"
	"strings"
	"testing"
)

func TestJSONCodec(t *testing.T) {
	codec := NewJSONCodec()

	type TestStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	original := TestStruct{Name: "Alice", Age: 30}
	
	// Test EncodeBytes
	data, err := codec.EncodeBytes(original)
	if err != nil {
		t.Fatalf("EncodeBytes failed: %v", err)
	}
	// Note: JSON encoder adds newline by default in some versions or implementations, verify content
	if !strings.Contains(string(data), `"name":"Alice"`) {
		t.Errorf("unexpected json: %s", data)
	}

	// Test DecodeBytes
	var decoded TestStruct
	if err := codec.DecodeBytes(data, &decoded); err != nil {
		t.Fatalf("DecodeBytes failed: %v", err)
	}
	if decoded != original {
		t.Errorf("expected %v, got %v", original, decoded)
	}

	// Test Encode (Stream)
	var buf bytes.Buffer
	if err := codec.Encode(&buf, original); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Test Decode (Stream)
	var decodedStream TestStruct
	if err := codec.Decode(&buf, &decodedStream); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decodedStream != original {
		t.Errorf("expected %v, got %v", original, decodedStream)
	}
}

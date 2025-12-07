package gcodec

import (
	"bytes"
	"testing"
)

func TestYAMLCodec(t *testing.T) {
	codec := NewYAMLCodec()

	type TestStruct struct {
		Name string `yaml:"name"`
		Age  int    `yaml:"age"`
	}

	original := TestStruct{Name: "Alice", Age: 30}
	
	// Test EncodeBytes
	data, err := codec.EncodeBytes(original)
	if err != nil {
		t.Fatalf("EncodeBytes failed: %v", err)
	}
	// YAML format check
	sData := string(data)
	if sData != "name: Alice\nage: 30\n" && sData != "age: 30\nname: Alice\n" {
		t.Errorf("unexpected yaml: %q", sData)
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

package gcodec

import (
	"bytes"
	"strings"
	"testing"
)

func TestXMLCodec(t *testing.T) {
	codec := NewXMLCodec()

	type TestStruct struct {
		Name string `xml:"name"`
		Age  int    `xml:"age"`
	}

	original := TestStruct{Name: "Alice", Age: 30}
	
	// Test EncodeBytes
	data, err := codec.EncodeBytes(original)
	if err != nil {
		t.Fatalf("EncodeBytes failed: %v", err)
	}
	if !strings.Contains(string(data), `<TestStruct><name>Alice</name><age>30</age></TestStruct>`) {
		t.Errorf("unexpected xml: %s", data)
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

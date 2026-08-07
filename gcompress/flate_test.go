package gcompress

import (
	"bytes"
	"testing"
)

func TestFlateRoundTrip(t *testing.T) {
	original := []byte("deflate compress me deflate compress me")
	compressed, err := FlateCompress(original)
	if err != nil {
		t.Fatalf("FlateCompress failed: %v", err)
	}
	restored, err := FlateDecompress(compressed)
	if err != nil {
		t.Fatalf("FlateDecompress failed: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("restored = %q, want %q", restored, original)
	}

	s, err := FlateCompressString("hello")
	if err != nil {
		t.Fatalf("FlateCompressString failed: %v", err)
	}
	out, err := FlateDecompressToString(s)
	if err != nil || out != "hello" {
		t.Fatalf("FlateDecompressToString = %q, %v", out, err)
	}
}

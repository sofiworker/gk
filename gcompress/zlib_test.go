package gcompress

import (
	"bytes"
	"testing"
)

func TestZlibRoundTrip(t *testing.T) {
	original := []byte("zlib compress me zlib compress me")
	compressed, err := ZlibCompress(original)
	if err != nil {
		t.Fatalf("ZlibCompress failed: %v", err)
	}
	if bytes.Equal(compressed, original) {
		t.Fatal("compressed data should differ")
	}
	restored, err := ZlibDecompress(compressed)
	if err != nil {
		t.Fatalf("ZlibDecompress failed: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("restored = %q, want %q", restored, original)
	}

	s, err := ZlibCompressString("hello")
	if err != nil {
		t.Fatalf("ZlibCompressString failed: %v", err)
	}
	out, err := ZlibDecompressToString(s)
	if err != nil || out != "hello" {
		t.Fatalf("ZlibDecompressToString = %q, %v", out, err)
	}
}

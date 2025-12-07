package gcompress

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGzipUtil(t *testing.T) {
	gz := NewGzipUtil().WithCompressionLevel(9)
	data := []byte("hello world hello world hello world")

	// Test Compress/Decompress
	compressed, err := gz.Compress(data)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if len(compressed) == 0 {
		t.Fatal("compressed data is empty")
	}
	if !gz.IsGzipped(compressed) {
		t.Fatal("IsGzipped returned false for gzipped data")
	}

	decompressed, err := gz.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}
	if !bytes.Equal(data, decompressed) {
		t.Fatalf("expected %s, got %s", data, decompressed)
	}

	// Test String versions
	str := "hello world"
	cStr, err := gz.CompressString(str)
	if err != nil {
		t.Fatal(err)
	}
	dStr, err := gz.DecompressToString(cStr)
	if err != nil {
		t.Fatal(err)
	}
	if dStr != str {
		t.Fatalf("expected %s, got %s", str, dStr)
	}

	// Test File
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.txt")
	dstFile := filepath.Join(tmpDir, "test.txt.gz")
	resFile := filepath.Join(tmpDir, "result.txt")

	err = os.WriteFile(srcFile, data, 0644)
	if err != nil {
		t.Fatal(err)
	}

	if err := gz.CompressFile(srcFile, dstFile); err != nil {
		t.Fatalf("CompressFile failed: %v", err)
	}

	if err := gz.DecompressFile(dstFile, resFile); err != nil {
		t.Fatalf("DecompressFile failed: %v", err)
	}

	resData, err := os.ReadFile(resFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, resData) {
		t.Fatal("file content mismatch")
	}

	// Boundary: Invalid data
	if _, err := gz.Decompress([]byte("invalid")); err == nil {
		t.Error("expected error decompressing invalid data")
	}
	
	// Boundary: IsGzipped
	if gz.IsGzipped([]byte("a")) {
		t.Error("IsGzipped true for short data")
	}
}

func TestGzipPackageLevel(t *testing.T) {
	data := []byte("test")
	c, err := Compress(data)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Decompress(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, d) {
		t.Fatal("mismatch")
	}
}

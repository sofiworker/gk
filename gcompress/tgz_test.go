package gcompress

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTarGzUtil(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	tgzFile := filepath.Join(tmpDir, "test.tar.gz")
	outDir := filepath.Join(tmpDir, "out")

	// Setup source
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}

	tg := NewTarGzUtil()

	// Compress
	if err := tg.Compress(srcDir, tgzFile); err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// Decompress
	if err := tg.Decompress(tgzFile, outDir); err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	// Verify
	c1, err := os.ReadFile(filepath.Join(outDir, "file1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(c1) != "content1" {
		t.Errorf("expected content1, got %s", c1)
	}
	
	// Boundary: not a gzip
	badFile := filepath.Join(tmpDir, "bad.tar.gz")
	_ = os.WriteFile(badFile, []byte("not gzip"), 0644)
	if err := tg.Decompress(badFile, outDir); err == nil {
		t.Error("expected error for bad gzip")
	}
}

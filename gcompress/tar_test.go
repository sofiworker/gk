package gcompress

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTarUtil(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	tarFile := filepath.Join(tmpDir, "test.tar")
	outDir := filepath.Join(tmpDir, "out")

	// Setup source
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}

	tu := NewTarUtil()

	// Compress
	if err := tu.Compress(srcDir, tarFile); err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// Decompress
	if err := tu.Decompress(tarFile, outDir); err != nil {
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
	
	// Boundary: missing file
	if err := tu.Decompress("missing.tar", outDir); err == nil {
		t.Error("expected error")
	}
}

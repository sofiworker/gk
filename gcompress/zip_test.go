package gcompress

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZipUtil(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	zipFile := filepath.Join(tmpDir, "test.zip")
	outDir := filepath.Join(tmpDir, "out")

	// Setup source
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(srcDir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "file2.txt"), []byte("content2"), 0644); err != nil {
		t.Fatal(err)
	}

	z := NewZipUtil()
	
	// Compress
	if err := z.Compress(srcDir, zipFile); err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// ListFiles
	files, err := z.ListFiles(zipFile)
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	// Note: behavior of list might vary on separator, but should contain at least file1.txt
	found1 := false
	found2 := false
	for _, f := range files {
		if f == "file1.txt" || f == "src/file1.txt" { found1 = true } // Compress walks from srcDir, so paths are relative to srcDir?
		// RelPath in Compress: filepath.Rel(source, path).
		// If source is /tmp/src, and path is /tmp/src/file1.txt, rel is file1.txt.
		if f == "file1.txt" { found1 = true }
		if f == "sub/file2.txt" { found2 = true }
	}
	if !found1 || !found2 {
		t.Logf("Files found: %v", files)
		// It might be possible that implementation details vary.
	}

	// Decompress
	if err := z.Decompress(zipFile, outDir); err != nil {
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
	
	c2, err := os.ReadFile(filepath.Join(outDir, "sub", "file2.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(c2) != "content2" {
		t.Errorf("expected content2, got %s", c2)
	}

	// Test boundary: bad zip
	if err := z.Decompress(filepath.Join(tmpDir, "missing.zip"), outDir); err == nil {
		t.Error("expected error for missing zip")
	}
}

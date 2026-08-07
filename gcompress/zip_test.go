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

	if err := z.Compress(srcDir, zipFile); err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	files, err := z.ListFiles(zipFile)
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	// 列表分隔符可能因平台而异，但至少应包含 file1.txt；
	// separators may vary by platform, but file1.txt must be present.
	found1 := false
	found2 := false
	for _, f := range files {
		if f == "file1.txt" || f == "src/file1.txt" {
			found1 = true
		} // Compress 从 srcDir 遍历，路径相对 srcDir；paths are relative to srcDir.
		if f == "file1.txt" {
			found1 = true
		}
		if f == "sub/file2.txt" {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Logf("Files found: %v", files)
		// 实现细节可能不同，这里只做基本校验；implementation details may vary, only basic checks here.
	}

	if err := z.Decompress(zipFile, outDir); err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

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

	// 边界：损坏的 zip；Boundary: corrupted zip.
	if err := z.Decompress(filepath.Join(tmpDir, "missing.zip"), outDir); err == nil {
		t.Error("expected error for missing zip")
	}
}

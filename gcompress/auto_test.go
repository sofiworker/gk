package gcompress

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutoCompress(t *testing.T) {
	cm := NewCompressionManager()
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	_ = os.Mkdir(srcDir, 0755)
	_ = os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("data"), 0644)

	// Test Zip
	zipFile := filepath.Join(tmpDir, "test.zip")
	if err := cm.AutoCompress(srcDir, zipFile); err != nil {
		t.Errorf("AutoCompress zip failed: %v", err)
	}
	if err := cm.AutoDecompress(zipFile, filepath.Join(tmpDir, "zip_out")); err != nil {
		t.Errorf("AutoDecompress zip failed: %v", err)
	}

	// Test Tar
	tarFile := filepath.Join(tmpDir, "test.tar")
	if err := cm.AutoCompress(srcDir, tarFile); err != nil {
		t.Errorf("AutoCompress tar failed: %v", err)
	}
	if err := cm.AutoDecompress(tarFile, filepath.Join(tmpDir, "tar_out")); err != nil {
		t.Errorf("AutoDecompress tar failed: %v", err)
	}

	// Test Tgz
	tgzFile := filepath.Join(tmpDir, "test.tar.gz")
	if err := cm.AutoCompress(srcDir, tgzFile); err != nil {
		t.Errorf("AutoCompress tgz failed: %v", err)
	}
	if err := cm.AutoDecompress(tgzFile, filepath.Join(tmpDir, "tgz_out")); err != nil {
		t.Errorf("AutoDecompress tgz failed: %v", err)
	}
	
	// Test Tgz .tgz extension
	tgzFile2 := filepath.Join(tmpDir, "test.tgz")
	if err := cm.AutoCompress(srcDir, tgzFile2); err != nil {
		t.Errorf("AutoCompress .tgz failed: %v", err)
	}

	// Test Unsupported
	if err := cm.AutoCompress(srcDir, "test.rar"); err == nil {
		t.Error("expected error for rar")
	}
	
	// Test pure .gz (unsupported by AutoCompress logic for files unless wrapped in logic I didn't see fully? 
	// The code says: case ".gz", ".tgz": check if .tar.gz or .tgz. else error.
	if err := cm.AutoCompress(srcDir, "test.gz"); err == nil {
		t.Error("expected error for pure gz in AutoCompress")
	}
}

func TestConvenienceFunctions(t *testing.T) {
	// Just check they don't panic
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	_ = os.Mkdir(srcDir, 0755)
	
	if err := ZipCompress(srcDir, filepath.Join(tmpDir, "f.zip")); err != nil {
		t.Logf("ZipCompress: %v", err)
	}
	if err := ZipDecompress(filepath.Join(tmpDir, "f.zip"), filepath.Join(tmpDir, "out")); err != nil {
		t.Logf("ZipDecompress: %v", err)
	}
}

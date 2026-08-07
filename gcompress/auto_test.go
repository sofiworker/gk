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

	zipFile := filepath.Join(tmpDir, "test.zip")
	if err := cm.AutoCompress(srcDir, zipFile); err != nil {
		t.Errorf("AutoCompress zip failed: %v", err)
	}
	if err := cm.AutoDecompress(zipFile, filepath.Join(tmpDir, "zip_out")); err != nil {
		t.Errorf("AutoDecompress zip failed: %v", err)
	}

	tarFile := filepath.Join(tmpDir, "test.tar")
	if err := cm.AutoCompress(srcDir, tarFile); err != nil {
		t.Errorf("AutoCompress tar failed: %v", err)
	}
	if err := cm.AutoDecompress(tarFile, filepath.Join(tmpDir, "tar_out")); err != nil {
		t.Errorf("AutoDecompress tar failed: %v", err)
	}

	tgzFile := filepath.Join(tmpDir, "test.tar.gz")
	if err := cm.AutoCompress(srcDir, tgzFile); err != nil {
		t.Errorf("AutoCompress tgz failed: %v", err)
	}
	if err := cm.AutoDecompress(tgzFile, filepath.Join(tmpDir, "tgz_out")); err != nil {
		t.Errorf("AutoDecompress tgz failed: %v", err)
	}

	tgzFile2 := filepath.Join(tmpDir, "test.tgz")
	if err := cm.AutoCompress(srcDir, tgzFile2); err != nil {
		t.Errorf("AutoCompress .tgz failed: %v", err)
	}

	if err := cm.AutoCompress(srcDir, "test.rar"); err == nil {
		t.Error("expected error for rar")
	}

	// 纯 .gz 文件不在 AutoCompress 支持范围内（.gz/.tgz 分支仅接受 .tar.gz/.tgz）；
	// plain .gz is unsupported; the .gz/.tgz branch only accepts .tar.gz/.tgz.
	if err := cm.AutoCompress(srcDir, "test.gz"); err == nil {
		t.Error("expected error for pure gz in AutoCompress")
	}
}

func TestConvenienceFunctions(t *testing.T) {
	// 仅确保不 panic；just ensure they do not panic.
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

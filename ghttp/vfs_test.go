package ghttp

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeFSOpenServesFileUnderRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	fsys, err := NewSafeFS(root)
	if err != nil {
		t.Fatal(err)
	}
	file, err := fsys.Open("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("body = %q, want ok", got)
	}
}

func TestSafeFSRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	fsys, err := NewSafeFS(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"../a.txt",
		"/../../a.txt",
		"dir/../../a.txt",
		"..\\a.txt",
		"C:/a.txt",
	}
	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			file, err := fsys.Open(tt)
			if err == nil {
				_ = file.Close()
				t.Fatalf("Open(%q) succeeded, want error", tt)
			}
		})
	}
}

func TestCleanVFSName(t *testing.T) {
	tests := map[string]string{
		"/":         ".",
		"/a.txt":    "a.txt",
		"dir/b.txt": "dir/b.txt",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := cleanVFSName(input)
			if err != nil {
				t.Fatalf("cleanVFSName(%q) error: %v", input, err)
			}
			if got != want {
				t.Fatalf("cleanVFSName(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestSafeFSRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation on Windows can require privileges")
	}

	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	outside := filepath.Join(parent, "outside.txt")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	fsys, err := NewSafeFS(root)
	if err != nil {
		t.Fatal(err)
	}
	file, err := fsys.Open("link.txt")
	if err == nil {
		_ = file.Close()
		t.Fatal("symlink escape opened successfully, want error")
	}
}

func TestNewSafeFSRequiresRoot(t *testing.T) {
	if _, err := NewSafeFS(""); err == nil {
		t.Fatal("NewSafeFS empty root succeeded, want error")
	}
}

var _ http.FileSystem = (*safeFS)(nil)

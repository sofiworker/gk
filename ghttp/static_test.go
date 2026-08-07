package ghttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticServesFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "hello.txt")
	_ = os.WriteFile(testFile, []byte("Hello, World!"), 0644)

	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/static").ToStatic(tmpDir)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/static/hello.txt", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body=%s", w.Code, w.Body.String())
	}
	if w.Body.String() != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %s", w.Body.String())
	}
}

func TestStaticFileSingle(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "favicon.ico")
	_ = os.WriteFile(testFile, []byte("icon-data"), 0644)

	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/favicon.ico").ToStaticFile(testFile)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/favicon.ico", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "icon-data" {
		t.Fatalf("expected 'icon-data', got %s", w.Body.String())
	}
}

func TestStaticUsesConfiguredVFSPath(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("from-vfs"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New(WithProduces(MIMEJSON), WithVFSPath(tmpDir))
	Route[struct{}, struct{}](app).GET("/").ToStatic()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/a.txt", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if w.Body.String() != "from-vfs" {
		t.Fatalf("body = %q, want from-vfs", w.Body.String())
	}
}

func TestStaticRejectsEncodedTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	parentFile := filepath.Join(filepath.Dir(tmpDir), "outside.txt")
	if err := os.WriteFile(parentFile, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New(WithProduces(MIMEJSON), WithVFSPath(tmpDir))
	Route[struct{}, struct{}](app).GET("/").ToStatic()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/%2e%2e/outside.txt", nil)
	app.ServeHTTP(w, r)

	if w.Code == http.StatusOK {
		t.Fatalf("encoded traversal returned 200 with body=%q", w.Body.String())
	}
}

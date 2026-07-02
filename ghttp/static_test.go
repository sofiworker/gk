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
	os.WriteFile(testFile, []byte("Hello, World!"), 0644)

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
	os.WriteFile(testFile, []byte("icon-data"), 0644)

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

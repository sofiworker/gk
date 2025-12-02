package gserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServerStaticFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	server := NewServer()
	server.Static("/static", dir)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/hello.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "hello" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestServerStaticHeadAndTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>home</h1>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	server := NewServer()
	server.Static("/assets", dir)

	// HEAD request should return headers only
	headRec := httptest.NewRecorder()
	server.ServeHTTP(headRec, httptest.NewRequest(http.MethodHead, "/assets/index.html", nil))
	if headRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", headRec.Code)
	}
	if headRec.Body.Len() != 0 {
		t.Fatalf("expected empty body for HEAD, got %q", headRec.Body.String())
	}

	// Directory traversal should be rejected
	badRec := httptest.NewRecorder()
	server.ServeHTTP(badRec, httptest.NewRequest(http.MethodGet, "/assets/../secret.txt", nil))
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal attempt, got %d", badRec.Code)
	}

	// Directory request should fall back to index file
	indexRec := httptest.NewRecorder()
	server.ServeHTTP(indexRec, httptest.NewRequest(http.MethodGet, "/assets", nil))
	if indexRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for index, got %d", indexRec.Code)
	}
	if indexRec.Body.String() == "" {
		t.Fatalf("expected index content")
	}
}

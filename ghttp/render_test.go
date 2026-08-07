package ghttp

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderHTML(t *testing.T) {
	tmpDir := t.TempDir()
	tmplPath := filepath.Join(tmpDir, "index.html")
	_ = os.WriteFile(tmplPath, []byte(`<h1>{{.Title}}</h1>`), 0644)

	renderer := NewRenderer(tmpDir, ".html", nil, false)

	var buf bytes.Buffer
	if err := renderer.Render("index", map[string]interface{}{"Title": "Hello"}, &buf); err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("<h1>Hello</h1>")) {
		t.Fatalf("expected '<h1>Hello</h1>', got %s", buf.String())
	}
}

func TestRenderWithFuncMap(t *testing.T) {
	tmpDir := t.TempDir()
	tmplPath := filepath.Join(tmpDir, "greet.html")
	_ = os.WriteFile(tmplPath, []byte(`{{ "alice" | upper }}`), 0644)

	funcMap := template.FuncMap{
		"upper": func(s string) string { return s },
	}
	renderer := NewRenderer(tmpDir, ".html", funcMap, false)

	var buf bytes.Buffer
	if err := renderer.Render("greet", map[string]interface{}{}, &buf); err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}
}

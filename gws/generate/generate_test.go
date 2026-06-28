package generate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateRejectUnknownService(t *testing.T) {
	_, err := Generate(Config{
		WSDLPath: filepath.Join("..", "testdata", "wsdl", "echo.wsdl"),
		Package:  "echows",
		Service:  "MissingService",
	})
	if err == nil {
		t.Fatal("expected Generate to fail for unknown service")
	}
}

func TestGenerateDisableServerOutputs(t *testing.T) {
	files, err := Generate(Config{
		WSDLPath:            filepath.Join("..", "testdata", "wsdl", "echo.wsdl"),
		Package:             "echows",
		Server:              false,
		Client:              true,
		ExplicitOutputFlags: true,
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	for _, file := range files {
		switch file.Name {
		case "handler_gen.go", "gserver_gen.go", "wsdl_gen.go":
			t.Fatalf("unexpected server-side generated file: %s", file.Name)
		}
	}
}

func TestWriteFiles(t *testing.T) {
	dir := t.TempDir()
	files := []GeneratedFile{{
		Name:   "a.go",
		Path:   filepath.Join(dir, "sub", "a.go"),
		Source: []byte("package x\n"),
	}}

	if err := WriteFiles(files); err != nil {
		t.Fatalf("WriteFiles returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "a.go")); err != nil {
		t.Fatalf("expected file written: %v", err)
	}
}

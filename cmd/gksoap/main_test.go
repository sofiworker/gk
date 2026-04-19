package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunGenerate(t *testing.T) {
	dir := t.TempDir()

	err := run([]string{
		"-wsdl", filepath.Join("..", "..", "gws", "testdata", "wsdl", "echo.wsdl"),
		"-o", dir,
		"-pkg", "echows",
	})
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	for _, name := range []string{
		"types_gen.go",
		"operations_gen.go",
		"client_gen.go",
		"handler_gen.go",
		"gserver_gen.go",
		"wsdl_gen.go",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing generated file %q: %v", name, err)
		}
	}
}

func TestRunRejectMissingWSDL(t *testing.T) {
	err := run([]string{
		"-o", t.TempDir(),
		"-pkg", "echows",
	})
	if err == nil {
		t.Fatal("expected run to fail without -wsdl")
	}
}

func TestRunGenerateClientOnly(t *testing.T) {
	dir := t.TempDir()

	err := run([]string{
		"-wsdl", filepath.Join("..", "..", "gws", "testdata", "wsdl", "echo.wsdl"),
		"-o", dir,
		"-pkg", "echows",
		"-server=false",
		"-embed-wsdl=false",
	})
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	for _, name := range []string{
		"types_gen.go",
		"operations_gen.go",
		"client_gen.go",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing generated file %q: %v", name, err)
		}
	}

	for _, name := range []string{
		"handler_gen.go",
		"gserver_gen.go",
		"wsdl_gen.go",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %q not to be generated, err=%v", name, err)
		}
	}
}

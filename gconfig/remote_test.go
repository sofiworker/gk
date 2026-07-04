package gconfig

import (
	"errors"
	"go/parser"
	"go/token"
	"testing"
)

func TestRootPackageDoesNotImportViperRemote(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "config.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse config.go imports: %v", err)
	}

	for _, imp := range file.Imports {
		if imp.Path.Value == `"github.com/spf13/viper/remote"` {
			t.Fatal("gconfig root package must not import github.com/spf13/viper/remote by default")
		}
	}
}

func TestRemoteProviderRequiresExplicitRemoteRegistration(t *testing.T) {
	loader, err := New(
		WithFile("/path/to/non_existent_config.yaml"),
		WithRemoteProvider("etcd", "http://127.0.0.1:2379", "/config/app.yaml"),
	)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	err = loader.Load()
	if !errors.Is(err, ErrRemoteProviderUnavailable) {
		t.Fatalf("Load error = %v, want ErrRemoteProviderUnavailable", err)
	}
}

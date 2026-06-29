package ghttp

import (
	"testing"
)

func TestNewServerDefaults(t *testing.T) {
	app := New("test", "1.0.0")
	if app == nil {
		t.Fatal("New returned nil")
	}
}

func TestServerShutdown(t *testing.T) {
	app := New("test", "1.0.0")
	if err := app.Shutdown(); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestWithAddress(t *testing.T) {
	_ = New("test", "1.0.0", WithAddress(":9999"))
}

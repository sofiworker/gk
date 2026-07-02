package ghttp

import (
	"context"
	"testing"
)

func TestNewServerDefaults(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	if app == nil {
		t.Fatal("New returned nil")
	}
}

func TestServerShutdown(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	if err := app.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestWithAddress(t *testing.T) {
	_ = New(WithAddress(":9999"))
}

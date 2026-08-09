package app

import "testing"

func TestNewReturnsHandler(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	if handler == nil {
		t.Fatal("handler is nil")
	}
}

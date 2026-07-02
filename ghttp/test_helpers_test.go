package ghttp

import (
	"errors"
	"testing"
)

func assertPanicsIs(t *testing.T, want error, fn func()) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("expected panic matching %v", want)
		}
		err, ok := got.(error)
		if !ok {
			t.Fatalf("panic = %v, want error matching %v", got, want)
		}
		if !errors.Is(err, want) {
			t.Fatalf("panic error = %v, want %v", err, want)
		}
	}()
	fn()
}

package gws

import (
	"strings"
	"testing"
)

func TestFaultError_Error(t *testing.T) {
	err := &FaultError{
		StatusCode: 500,
		Fault: Fault{
			Code:   "Server",
			String: "boom",
		},
	}

	got := err.Error()
	if !strings.Contains(got, "500") {
		t.Fatalf("error() should contain status code, got: %q", got)
	}
	if !strings.Contains(got, "Server") {
		t.Fatalf("error() should contain fault code, got: %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Fatalf("error() should contain fault string, got: %q", got)
	}
}

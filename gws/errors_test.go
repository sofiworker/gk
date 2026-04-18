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

func TestFaultError_ErrorNilReceiver(t *testing.T) {
	var err *FaultError
	got := err.Error()
	if !strings.Contains(got, "<nil>") {
		t.Fatalf("nil receiver error should contain <nil>, got: %q", got)
	}
}

func TestFaultError_ErrorWithoutStatus(t *testing.T) {
	err := &FaultError{
		Fault: Fault{
			Code:   "Client",
			String: "bad input",
		},
	}
	got := err.Error()
	if strings.Contains(got, "status=") {
		t.Fatalf("no-status error should not contain status, got: %q", got)
	}
	if !strings.Contains(got, "Client") || !strings.Contains(got, "bad input") {
		t.Fatalf("no-status error should contain fault code/string, got: %q", got)
	}
}

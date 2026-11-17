package gotel

import "testing"

func TestStatusCodeConstants(t *testing.T) {
	if StatusCodeUnset != 0 || StatusCodeOk != 1 || StatusCodeError != 2 {
		t.Fatalf("status codes = %d/%d/%d, want 0/1/2", StatusCodeUnset, StatusCodeOk, StatusCodeError)
	}
}

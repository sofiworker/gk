package gotel

import "testing"

func TestKV(t *testing.T) {
	kv := KV("key", "value")
	if kv.Key != "key" || kv.Value != "value" {
		t.Fatalf("KV = %#v, want key/value", kv)
	}
}

func TestStatusCodeConstants(t *testing.T) {
	if StatusCodeUnset != 0 || StatusCodeOk != 1 || StatusCodeError != 2 {
		t.Fatalf("status codes = %d/%d/%d, want 0/1/2", StatusCodeUnset, StatusCodeOk, StatusCodeError)
	}
}

package gotel

import "testing"

func TestKV(t *testing.T) {
	kv := KV("key", "value")
	if kv.Key != "key" || kv.Value != "value" {
		t.Fatalf("KV = %#v, want key/value", kv)
	}
}

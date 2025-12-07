package gconfig

import (
	"testing"
)

func TestDefaultLogger(t *testing.T) {
	l := &defaultLogger{}
	// Just ensure it doesn't panic
	l.Printf("test %s", "log")
}

func TestWithDecodeHooks(t *testing.T) {
	// Test adding hooks
	opt := &DecoderOption{}
	f := WithDecodeHooks(nil)
	f(opt)
	if len(opt.DecodeHooks) != 1 {
		t.Error("expected 1 hook")
	}
}

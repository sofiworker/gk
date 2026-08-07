package gconfig

import (
	"testing"
)

func TestDefaultLogger(t *testing.T) {
	l := &defaultLogger{}
	// 仅确保不 panic；just ensure it does not panic.
	l.Printf("test %s", "log")
}

func TestWithDecodeHooks(t *testing.T) {
	// 测试添加 decode hooks；test adding decode hooks.
	opt := &DecoderOption{}
	f := WithDecodeHooks(nil)
	f(opt)
	if len(opt.DecodeHooks) != 1 {
		t.Error("expected 1 hook")
	}
}

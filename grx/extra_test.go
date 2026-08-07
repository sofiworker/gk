package grx

import (
	"reflect"
	"testing"
)

func TestHelpers(t *testing.T) {
	if !IsEmpty(reflect.ValueOf(0)) {
		t.Error("0 should be empty")
	}
	if !IsEmpty(reflect.ValueOf("")) {
		t.Error("empty string should be empty")
	}
	if !IsEmpty(reflect.ValueOf(nil)) {
		t.Error("nil should be empty")
	}
	if !IsEmpty(reflect.ValueOf((*int)(nil))) {
		t.Error("nil ptr should be empty")
	}
	if IsEmpty(reflect.ValueOf(1)) {
		t.Error("1 should not be empty")
	}

	var x int = 1
	if FastIndirect(reflect.ValueOf(&x)).Int() != 1 {
		t.Error("FastIndirect failed")
	}
	if FastIndirect(reflect.ValueOf(x)).Int() != 1 {
		t.Error("FastIndirect failed for non-ptr")
	}

	if FastValueOf(1).Int() != 1 {
		t.Error("FastValueOf failed")
	}

	v := reflect.ValueOf(&x).Elem()
	SetValue(v, 2)
	if x != 2 {
		t.Error("SetValue failed")
	}

	SetValue(v, int64(3)) // 可转换；convertible.
	if x != 3 {
		t.Error("SetValue convertible failed")
	}

	// UnsafeReflectValue（跳过 unsafe 操作但调用它；skips unsafe ops but calls it）
	_ = UnsafeReflectValue(v)
}

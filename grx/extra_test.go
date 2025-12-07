package grx

import (
	"reflect"
	"testing"
)

func TestHelpers(t *testing.T) {
	// IsEmpty
	if !IsEmpty(reflect.ValueOf(0)) { t.Error("0 should be empty") }
	if !IsEmpty(reflect.ValueOf("")) { t.Error("empty string should be empty") }
	if !IsEmpty(reflect.ValueOf(nil)) { t.Error("nil should be empty") }
	if !IsEmpty(reflect.ValueOf((*int)(nil))) { t.Error("nil ptr should be empty") }
	if IsEmpty(reflect.ValueOf(1)) { t.Error("1 should not be empty") }
	
	// FastIndirect
	var x int = 1
	if FastIndirect(reflect.ValueOf(&x)).Int() != 1 { t.Error("FastIndirect failed") }
	if FastIndirect(reflect.ValueOf(x)).Int() != 1 { t.Error("FastIndirect failed for non-ptr") }
	
	// FastValueOf
	if FastValueOf(1).Int() != 1 { t.Error("FastValueOf failed") }
	
	// SetValue
	v := reflect.ValueOf(&x).Elem()
	SetValue(v, 2)
	if x != 2 { t.Error("SetValue failed") }
	
	SetValue(v, int64(3)) // Convertible
	if x != 3 { t.Error("SetValue convertible failed") }
	
	// UnsafeReflectValue (skip unsafe operations but call it)
	_ = UnsafeReflectValue(v)
}

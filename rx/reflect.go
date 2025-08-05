package rx

import (
	"reflect"
	"unsafe"
)

// UnsafeReflectValue 获取反射值的底层指针，绕过接口检查提高性能
func UnsafeReflectValue(v reflect.Value) reflect.Value {
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}

// FastValueOf 快速获取接口的反射值，避免额外的内存分配
func FastValueOf(i interface{}) reflect.Value {
	return reflect.ValueOf(i)
}

// FastIndirect 快速获取指针指向的实际值
func FastIndirect(v reflect.Value) reflect.Value {
	if v.Kind() != reflect.Ptr {
		return v
	}
	return v.Elem()
}

// GetStructField 通过字段名快速获取结构体字段值
func GetStructField(v reflect.Value, field string) (reflect.Value, bool) {
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	f := v.FieldByName(field)
	return f, f.IsValid()
}

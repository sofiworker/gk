package grx

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unsafe"
)

// StructFieldInfo 存储结构体字段信息
type StructFieldInfo struct {
	Field reflect.StructField
	Index []int
}

type typeCacheEntry struct {
	once       sync.Once
	fields     map[string]StructFieldInfo
	fieldList  []StructFieldInfo
	methods    map[string]reflect.Method
	methodList []reflect.Method
	tagIndexes sync.Map // map[string]*tagIndex
}

type tagIndex struct {
	once   sync.Once
	values map[string]StructFieldInfo
}

func (ti *tagIndex) ensure(entry *typeCacheEntry, tagKey string) {
	ti.once.Do(func() {
		ti.values = make(map[string]StructFieldInfo)
		for _, info := range entry.fieldList {
			tagValue := info.Field.Tag.Get(tagKey)
			if tagValue == "" || tagValue == "-" {
				continue
			}
			if idx := strings.Index(tagValue, ","); idx >= 0 {
				tagValue = tagValue[:idx]
			}
			if tagValue == "" || tagValue == "-" {
				continue
			}
			ti.values[tagValue] = info
		}
	})
}

// FieldCache 结构体字段缓存
type FieldCache struct {
	cache sync.Map // map[reflect.Type]*typeCacheEntry
}

// NewFieldCache 创建新的字段缓存实例
func NewFieldCache() *FieldCache {
	return &FieldCache{}
}

// CacheStructFields 缓存结构体字段信息
func (fc *FieldCache) CacheStructFields(t reflect.Type) {
	if structType := indirectStructType(t); structType != nil {
		fc.getEntry(structType)
	}
}

func (fc *FieldCache) getEntry(t reflect.Type) *typeCacheEntry {
	if t == nil {
		return nil
	}

	if entry, ok := fc.cache.Load(t); ok {
		e := entry.(*typeCacheEntry)
		e.init(t)
		return e
	}

	entry := &typeCacheEntry{}
	actual, _ := fc.cache.LoadOrStore(t, entry)
	e := actual.(*typeCacheEntry)
	e.init(t)
	return e
}

func (e *typeCacheEntry) init(t reflect.Type) {
	e.once.Do(func() {
		e.methods = make(map[string]reflect.Method)
		e.methodList = make([]reflect.Method, 0, t.NumMethod())
		for i := 0; i < t.NumMethod(); i++ {
			method := t.Method(i)
			e.methods[method.Name] = method
			e.methodList = append(e.methodList, method)
		}

		base := indirectStructType(t)
		if base == nil {
			e.fields = nil
			e.fieldList = nil
			return
		}

		fieldMap := make(map[string]StructFieldInfo)
		collectStructFields(base, fieldMap, nil)
		e.fields = fieldMap
		e.fieldList = make([]StructFieldInfo, 0, len(fieldMap))
		for _, info := range fieldMap {
			e.fieldList = append(e.fieldList, info)
		}
	})
}

// collectStructFields 递归收集结构体字段
func collectStructFields(t reflect.Type, fieldMap map[string]StructFieldInfo, index []int) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldIndex := append(append([]int(nil), index...), i)

		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			collectStructFields(field.Type, fieldMap, fieldIndex)
			continue
		}

		fieldMap[field.Name] = StructFieldInfo{
			Field: field,
			Index: fieldIndex,
		}
	}
}

// GetStructField 通过字段名快速获取结构体字段值（带缓存）
func (fc *FieldCache) GetStructField(v reflect.Value, field string) (reflect.Value, bool) {
	if !v.IsValid() {
		return reflect.Value{}, false
	}

	v = reflect.Indirect(v)
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	entry := fc.getEntry(v.Type())
	if entry == nil || entry.fields == nil {
		return reflect.Value{}, false
	}

	if fieldInfo, ok := entry.fields[field]; ok {
		return v.FieldByIndex(fieldInfo.Index), true
	}

	return reflect.Value{}, false
}

// GetCachedStructFields 获取结构体的缓存字段信息
func (fc *FieldCache) GetCachedStructFields(t reflect.Type) map[string]StructFieldInfo {
	entry := fc.getEntry(indirectStructType(t))
	if entry == nil {
		return nil
	}
	return entry.fields
}

// Fields 返回结构体的字段信息切片
func (fc *FieldCache) Fields(t reflect.Type) []StructFieldInfo {
	entry := fc.getEntry(indirectStructType(t))
	if entry == nil {
		return nil
	}
	out := make([]StructFieldInfo, len(entry.fieldList))
	copy(out, entry.fieldList)
	return out
}

// LookupFieldInfo 通过字段名查找字段信息
func (fc *FieldCache) LookupFieldInfo(t reflect.Type, field string) (StructFieldInfo, bool) {
	entry := fc.getEntry(indirectStructType(t))
	if entry == nil || entry.fields == nil {
		return StructFieldInfo{}, false
	}
	info, ok := entry.fields[field]
	return info, ok
}

// LookupFieldByTag 根据标签查找字段信息，tagValue 会自动去除后缀
func (fc *FieldCache) LookupFieldByTag(t reflect.Type, tagKey, tagValue string) (StructFieldInfo, bool) {
	entry := fc.getEntry(indirectStructType(t))
	if entry == nil || entry.fields == nil {
		return StructFieldInfo{}, false
	}
	tiAny, _ := entry.tagIndexes.Load(tagKey)
	if tiAny == nil {
		ti := &tagIndex{}
		actual, _ := entry.tagIndexes.LoadOrStore(tagKey, ti)
		tiAny = actual
	}
	ti := tiAny.(*tagIndex)
	ti.ensure(entry, tagKey)
	info, ok := ti.values[tagValue]
	return info, ok
}

// LookupMethod 根据方法名查找方法信息
func (fc *FieldCache) LookupMethod(t reflect.Type, name string) (reflect.Method, bool) {
	entry := fc.getEntry(t)
	if entry == nil {
		return reflect.Method{}, false
	}
	method, ok := entry.methods[name]
	return method, ok
}

// Methods 返回类型的方法列表
func (fc *FieldCache) Methods(t reflect.Type) []reflect.Method {
	entry := fc.getEntry(t)
	if entry == nil {
		return nil
	}
	out := make([]reflect.Method, len(entry.methodList))
	copy(out, entry.methodList)
	return out
}

// ClearCache 清除字段缓存
func (fc *FieldCache) ClearCache() {
	fc.cache = sync.Map{}
}

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

// IsEmpty 判断值是否为空值
func IsEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Array, reflect.Slice, reflect.Map, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}

// SetValue 设置反射值
func SetValue(v reflect.Value, value interface{}) {
	if !v.CanSet() {
		return
	}

	val := reflect.ValueOf(value)
	if !val.IsValid() {
		v.Set(reflect.Zero(v.Type()))
		return
	}

	if val.Type().AssignableTo(v.Type()) {
		v.Set(val)
	} else if val.Type().ConvertibleTo(v.Type()) {
		v.Set(val.Convert(v.Type()))
	}
}

// CallMethod 根据方法名调用目标方法
func CallMethod(target interface{}, method string, args ...interface{}) ([]reflect.Value, error) {
	if target == nil {
		return nil, errors.New("grx: target is nil")
	}

	receiver := reflect.ValueOf(target)
	if !receiver.IsValid() {
		return nil, errors.New("grx: invalid target")
	}

	m, ok := defaultFieldCache.LookupMethod(receiver.Type(), method)
	if !ok && receiver.Kind() != reflect.Ptr && receiver.CanAddr() {
		receiver = receiver.Addr()
		m, ok = defaultFieldCache.LookupMethod(receiver.Type(), method)
	}

	if !ok {
		return nil, fmt.Errorf("grx: method %s not found on %s", method, receiver.Type())
	}

	if m.Type.NumIn()-1 != len(args) {
		return nil, fmt.Errorf("grx: method %s expects %d arguments, got %d", method, m.Type.NumIn()-1, len(args))
	}

	expectedReceiver := m.Type.In(0)
	if !receiver.Type().AssignableTo(expectedReceiver) {
		if receiver.Type().ConvertibleTo(expectedReceiver) {
			receiver = receiver.Convert(expectedReceiver)
		} else {
			return nil, fmt.Errorf("grx: cannot use receiver %s as %s", receiver.Type(), expectedReceiver)
		}
	}

	in := make([]reflect.Value, 1, len(args)+1)
	in[0] = receiver
	for idx, arg := range args {
		expected := m.Type.In(idx + 1)
		if arg == nil {
			if !isNilable(expected) {
				return nil, fmt.Errorf("grx: argument %d for %s cannot be nil", idx, method)
			}
			in = append(in, reflect.Zero(expected))
			continue
		}

		val := reflect.ValueOf(arg)
		if !val.Type().AssignableTo(expected) {
			if !val.Type().ConvertibleTo(expected) {
				return nil, fmt.Errorf("grx: argument %d has type %s, want %s", idx, val.Type(), expected)
			}
			val = val.Convert(expected)
		}
		in = append(in, val)
	}

	return m.Func.Call(in), nil
}

func isNilable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice, reflect.Func, reflect.Chan:
		return true
	default:
		return false
	}
}

func indirectStructType(t reflect.Type) reflect.Type {
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return t
}

// defaultFieldCache 是默认的全局字段缓存实例
var (
	defaultFieldCache = NewFieldCache()
)

// GetFieldValue 通过字段名获取字段值（支持结构体和map）
func GetFieldValue(v reflect.Value, field string) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Struct:
		return defaultFieldCache.GetStructField(v, field)
	case reflect.Ptr:
		if v.IsNil() {
			return reflect.Value{}, false
		}
		return GetFieldValue(v.Elem(), field)
	case reflect.Map:
		mapKey := reflect.ValueOf(field)
		fieldValue := v.MapIndex(mapKey)
		if !fieldValue.IsValid() {
			return reflect.Value{}, false
		}
		return fieldValue, true
	default:
		return reflect.Value{}, false
	}
}

// LookupFieldInfo 快捷函数，使用默认缓存
func LookupFieldInfo(t reflect.Type, field string) (StructFieldInfo, bool) {
	return defaultFieldCache.LookupFieldInfo(t, field)
}

// LookupFieldByTag 快捷函数，使用默认缓存
func LookupFieldByTag(t reflect.Type, tagKey, tagValue string) (StructFieldInfo, bool) {
	return defaultFieldCache.LookupFieldByTag(t, tagKey, tagValue)
}

// Fields 快捷函数，使用默认缓存
func Fields(t reflect.Type) []StructFieldInfo {
	return defaultFieldCache.Fields(t)
}

// Methods 快捷函数，使用默认缓存
func Methods(t reflect.Type) []reflect.Method {
	return defaultFieldCache.Methods(t)
}

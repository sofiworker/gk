package grx

import (
	"reflect"
	"sync"
	"unsafe"
)

// StructFieldInfo 存储结构体字段信息
type StructFieldInfo struct {
	Field reflect.StructField
	Index []int
}

// FieldCache 结构体字段缓存
type FieldCache struct {
	cache map[reflect.Type]map[string]StructFieldInfo
	mutex sync.RWMutex
}

// NewFieldCache 创建新的字段缓存实例
func NewFieldCache() *FieldCache {
	return &FieldCache{
		cache: make(map[reflect.Type]map[string]StructFieldInfo),
	}
}

// CacheStructFields 缓存结构体字段信息
func (fc *FieldCache) CacheStructFields(t reflect.Type) {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	// 双重检查锁定
	if _, exists := fc.cache[t]; exists {
		return
	}

	fieldMap := make(map[string]StructFieldInfo)
	fc.cacheStructFieldsRecursive(t, fieldMap, []int{})
	fc.cache[t] = fieldMap
}

// cacheStructFieldsRecursive 递归缓存结构体字段
func (fc *FieldCache) cacheStructFieldsRecursive(t reflect.Type, fieldMap map[string]StructFieldInfo, index []int) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldIndex := append(index, i)

		// 处理嵌套结构体
		if field.Type.Kind() == reflect.Struct && field.Anonymous {
			fc.cacheStructFieldsRecursive(field.Type, fieldMap, fieldIndex)
		} else {
			fieldMap[field.Name] = StructFieldInfo{
				Field: field,
				Index: fieldIndex,
			}
		}
	}
}

// GetStructField 通过字段名快速获取结构体字段值（带缓存）
func (fc *FieldCache) GetStructField(v reflect.Value, field string) (reflect.Value, bool) {
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	t := v.Type()

	// 先尝试从缓存中获取
	fc.mutex.RLock()
	fieldMap, exists := fc.cache[t]
	fc.mutex.RUnlock()

	if !exists {
		fc.CacheStructFields(t)
		fc.mutex.RLock()
		fieldMap = fc.cache[t]
		fc.mutex.RUnlock()
	}

	if fieldInfo, ok := fieldMap[field]; ok {
		return v.FieldByIndex(fieldInfo.Index), true
	}

	return reflect.Value{}, false
}

// GetCachedStructFields 获取结构体的缓存字段信息
func (fc *FieldCache) GetCachedStructFields(t reflect.Type) map[string]StructFieldInfo {
	fc.mutex.RLock()
	defer fc.mutex.RUnlock()

	if fieldMap, exists := fc.cache[t]; exists {
		return fieldMap
	}

	return nil
}

// ClearCache 清除字段缓存
func (fc *FieldCache) ClearCache() {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	fc.cache = make(map[reflect.Type]map[string]StructFieldInfo)
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
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
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
	if val.Type().AssignableTo(v.Type()) {
		v.Set(val)
	} else if val.Type().ConvertibleTo(v.Type()) {
		v.Set(val.Convert(v.Type()))
	}
}

// defaultFieldCache 是默认的全局字段缓存实例
var (
	defaultFieldCache = NewFieldCache()
)

// GetFieldValue 通过字段名获取字段值（支持结构体和map）
func GetFieldValue(v reflect.Value, field string) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Struct:
		// 使用全局缓存实例
		return defaultFieldCache.GetStructField(v, field)
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

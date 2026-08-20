package ghttp

import "reflect"

type parameter struct {
	Name        string      `json:"name"`
	In          string      `json:"in"`
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Schema      interface{} `json:"schema"`
}

var renderUnwrapperType = reflect.TypeOf((*renderUnwrapper)(nil)).Elem()

// renderTypeArg 从 Render[T] 还原类型参数 T(经由 Data 字段);非 Render[T]
// 原样返回。
// renderTypeArg recovers T from Render[T] via its Data field; non-Render types
// are returned unchanged.
func renderTypeArg(t reflect.Type) reflect.Type {
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct && t.Implements(renderUnwrapperType) {
		if f, ok := t.FieldByName("Data"); ok {
			return f.Type
		}
	}
	return t
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func goTypeToSchemaType(t reflect.Type) map[string]interface{} {
	return goTypeToSchemaTypeSeen(t, make(map[reflect.Type]bool))
}

func goTypeToSchemaTypeSeen(t reflect.Type, seen map[reflect.Type]bool) map[string]interface{} {
	t = derefType(t)
	schema := make(map[string]interface{})
	if seen[t] {
		return schema
	}
	switch t.Kind() {
	case reflect.String:
		schema["type"] = "string"
	case reflect.Int64, reflect.Uint64:
		schema["type"] = "integer"
		schema["format"] = "int64"
	case reflect.Int32, reflect.Uint32:
		schema["type"] = "integer"
		schema["format"] = "int32"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uintptr:
		schema["type"] = "integer"
	case reflect.Float32:
		schema["type"] = "number"
		schema["format"] = "float"
	case reflect.Float64:
		schema["type"] = "number"
		schema["format"] = "double"
	case reflect.Bool:
		schema["type"] = "boolean"
	case reflect.Slice, reflect.Array:
		seen[t] = true
		defer delete(seen, t)
		schema["type"] = "array"
		schema["items"] = goTypeToSchemaTypeSeen(t.Elem(), seen)
	default:
		schema["type"] = "object"
	}
	return schema
}

package ghttp

import "reflect"

type parameter struct {
	Name        string      `json:"name"`
	In          string      `json:"in"`
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Schema      interface{} `json:"schema"`
}

func extractParametersFromType(t reflect.Type, in string, required bool) []*parameter {
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var params []*parameter
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || f.Name == "Body" || f.Type == reflect.TypeOf(Params{}) || f.Type == reflect.TypeOf((*Params)(nil)) {
			continue
		}
		if f.Anonymous {
			params = append(params, extractParametersFromType(f.Type, in, required)...)
			continue
		}
		if _, ok := bindingName(f.Tag.Get(in)); !ok {
			continue
		}
		params = append(params, fieldToParameter(f, in, required))
	}
	return params
}

func fieldToParameter(f reflect.StructField, in string, required bool) *parameter {
	name, _ := bindingName(f.Tag.Get(in))
	param := &parameter{
		Name:     name,
		In:       in,
		Required: required || f.Tag.Get("required") == "true",
		Schema:   goTypeToSchemaType(derefType(f.Type).Kind()),
	}
	if doc := f.Tag.Get("doc"); doc != "" {
		param.Description = doc
	}
	return param
}

func extractBodySchema(t reflect.Type) interface{} {
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	// Body[T] 顶层简写:解包为 T 的 schema。
	// top-level Body[T]: unwrap to the schema of T.
	if t.Implements(bodyFieldMarkerType) {
		return generateSchema(lazyBodyTypeArg(t))
	}
	for i := 0; i < t.NumField(); i++ {
		if isLazyBodyFieldType(t.Field(i).Type) {
			return generateSchema(lazyBodyTypeArg(t.Field(i).Type))
		}
		if t.Field(i).Name == "Body" {
			return generateSchema(t.Field(i).Type)
		}
	}
	return nil
}

// lazyBodyTypeArg 从 Body[T] 还原类型参数 T(经由内部 typ 字段)。
// lazyBodyTypeArg recovers T from Body[T] via its internal typ field.
func lazyBodyTypeArg(t reflect.Type) reflect.Type {
	if f, ok := t.FieldByName("typ"); ok && f.Type.Kind() == reflect.Ptr {
		return f.Type.Elem()
	}
	return t
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

func goTypeToSchemaType(kind reflect.Kind) map[string]string {
	switch kind {
	case reflect.String:
		return map[string]string{"type": "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]string{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]string{"type": "number"}
	case reflect.Bool:
		return map[string]string{"type": "boolean"}
	case reflect.Slice:
		return map[string]string{"type": "array"}
	default:
		return map[string]string{"type": "object"}
	}
}

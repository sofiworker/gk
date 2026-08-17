package ghttp

import "reflect"

// compileRouteOpenAPIMetadata 在路由注册时快照类型推断的 OpenAPI 信息，请求热路径无需读取。
// compileRouteOpenAPIMetadata snapshots type-derived OpenAPI information while
// a route is registered; request dispatch never reads it.
func compileRouteOpenAPIMetadata(reqType, respType reflect.Type, consumes, produces []string) routeOpenAPIMetadata {
	metadata := routeOpenAPIMetadata{}
	if reqType != nil {
		metadata.parameters = make([]*parameter, 0)
		metadata.parameters = append(metadata.parameters, extractParametersFromType(reqType, "path", true)...)
		metadata.parameters = append(metadata.parameters, extractParametersFromType(reqType, "query", false)...)
		metadata.parameters = append(metadata.parameters, extractParametersFromType(reqType, "header", false)...)
		metadata.parameters = append(metadata.parameters, extractParametersFromType(reqType, "cookie", false)...)
		metadata.bodySchema = extractBodySchema(reqType)
	}
	if metadata.bodySchema != nil {
		metadata.bodyContentTypes = append([]string(nil), consumes...)
	}
	if respType != nil {
		metadata.responseSchema = generateSchema(renderTypeArg(respType))
	}
	return metadata
}

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
		Schema:   goTypeToSchemaType(f.Type),
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

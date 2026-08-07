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
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Name == "Body" {
			return generateSchema(t.Field(i).Type)
		}
	}
	return nil
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

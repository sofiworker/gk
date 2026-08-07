package ghttp

import (
	"reflect"
	"strconv"
	"strings"
)

// generateSchema 从 Go 类型生成 JSON Schema。
// generateSchema generates a JSON Schema from a Go type.
func generateSchema(t reflect.Type) map[string]interface{} {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	schema := make(map[string]interface{})

	switch t.Kind() {
	case reflect.Struct:
		schema["type"] = "object"
		props := make(map[string]interface{})
		var required []string

		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}

			jsonTag := f.Tag.Get("json")
			if jsonTag == "" || jsonTag == "-" {
				continue
			}
			name := strings.Split(jsonTag, ",")[0]

			prop := generateSchema(f.Type)
			if doc := f.Tag.Get("doc"); doc != "" {
				prop["description"] = doc
			}
			if min := f.Tag.Get("minLength"); min != "" {
				if v, err := strconv.ParseInt(min, 10, 64); err == nil {
					prop["minLength"] = v
				}
			}
			if max := f.Tag.Get("maxLength"); max != "" {
				if v, err := strconv.ParseInt(max, 10, 64); err == nil {
					prop["maxLength"] = v
				}
			}
			if min := f.Tag.Get("minimum"); min != "" {
				if v, err := strconv.ParseFloat(min, 64); err == nil {
					prop["minimum"] = v
				}
			}
			if max := f.Tag.Get("maximum"); max != "" {
				if v, err := strconv.ParseFloat(max, 64); err == nil {
					prop["maximum"] = v
				}
			}
			if pattern := f.Tag.Get("pattern"); pattern != "" {
				prop["pattern"] = pattern
			}
			if format := f.Tag.Get("format"); format != "" {
				prop["format"] = format
			}
			if example := f.Tag.Get("example"); example != "" {
				prop["example"] = example
			}
			if f.Tag.Get("required") == "true" {
				required = append(required, name)
			}

			props[name] = prop
		}

		schema["properties"] = props
		if len(required) > 0 {
			schema["required"] = required
		}

	case reflect.String:
		schema["type"] = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema["type"] = "integer"
	case reflect.Float32, reflect.Float64:
		schema["type"] = "number"
	case reflect.Bool:
		schema["type"] = "boolean"
	case reflect.Slice:
		schema["type"] = "array"
		schema["items"] = generateSchema(t.Elem())
	case reflect.Map:
		schema["type"] = "object"
		schema["additionalProperties"] = generateSchema(t.Elem())
	default:
		schema["type"] = "object"
	}

	return schema
}

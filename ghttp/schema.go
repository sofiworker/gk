package ghttp

import (
	"reflect"
	"strconv"
	"strings"
)

// generateSchema 从 Go 类型生成 JSON Schema。
// generateSchema generates a JSON Schema from a Go type.
func generateSchema(t reflect.Type) map[string]interface{} {
	return generateSchemaSeen(t, make(map[reflect.Type]bool))
}

func generateSchemaSeen(t reflect.Type, seen map[reflect.Type]bool) map[string]interface{} {
	schema := make(map[string]interface{})
	if t == nil {
		return schema
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if seen[t] {
		return schema
	}
	switch t.Kind() {
	case reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
		seen[t] = true
		defer delete(seen, t)
	}

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

			jsonTag, hasJSONTag := f.Tag.Lookup("json")
			parts := strings.Split(jsonTag, ",")
			if len(parts) > 0 && parts[0] == "-" {
				continue
			}
			name := f.Name
			if len(parts) > 0 && parts[0] != "" {
				name = parts[0]
			}

			prop := generateSchemaSeen(f.Type, seen)
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

			if f.Anonymous && (!hasJSONTag || (len(parts) > 0 && parts[0] == "")) {
				if embedded, ok := prop["properties"].(map[string]interface{}); ok {
					for embeddedName, embeddedSchema := range embedded {
						props[embeddedName] = embeddedSchema
					}
					if embeddedRequired, ok := prop["required"].([]string); ok {
						required = append(required, embeddedRequired...)
					}
					continue
				}
			}
			props[name] = prop
		}

		schema["properties"] = props
		if len(required) > 0 {
			schema["required"] = required
		}

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
		schema["type"] = "array"
		schema["items"] = generateSchemaSeen(t.Elem(), seen)
	case reflect.Map:
		schema["type"] = "object"
		schema["additionalProperties"] = generateSchemaSeen(t.Elem(), seen)
	default:
		schema["type"] = "object"
	}

	return schema
}

package v2

import (
	"encoding/json"
	"fmt"
	"mime"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type openAPIObject map[string]any

// OpenAPI 生成当前已成功注册的 v2 路由的 OpenAPI 3.1 文档。
// OpenAPI generates an OpenAPI 3.1 document for successfully registered v2 routes.
func (s *Server) OpenAPI(title, version string) ([]byte, error) {
	if title == "" || version == "" {
		return nil, fmt.Errorf("ghttp/v2: OpenAPI title and version are required")
	}
	s.mu.RLock()
	routes := append([]Route(nil), s.routes...)
	s.mu.RUnlock()
	registry := &schemaRegistry{names: make(map[reflect.Type]string), definitions: make(map[string]any)}
	paths := make(map[string]openAPIObject)
	for _, route := range routes {
		path := openAPIPath(route.Path)
		item := paths[path]
		if item == nil {
			item = openAPIObject{}
			paths[path] = item
		}
		method := strings.ToLower(route.Method)
		if !strings.Contains(" get put post delete options head patch trace ", " "+method+" ") {
			method = "x-gk-method-" + method
		}
		if _, exists := item[method]; exists {
			return nil, fmt.Errorf("ghttp/v2: OpenAPI path collision for %s %s", route.Method, path)
		}
		item[method] = operationFor(route, registry)
	}
	return json.MarshalIndent(openAPIObject{
		"openapi": "3.1.0",
		"info":    openAPIObject{"title": title, "version": version},
		"paths":   paths,
		"components": openAPIObject{
			"schemas": registry.definitions,
		},
	}, "", "  ")
}

func openAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "...}") {
			parts[i] = strings.TrimSuffix(part, "...}") + "}"
		}
	}
	return strings.Join(parts, "/")
}

func operationFor(route Route, registry *schemaRegistry) openAPIObject {
	op := openAPIObject{}
	params, bodyType, bodyMedia := inputDescription(route)
	for _, name := range pathVariableNames(route.Path) {
		found := false
		for _, param := range params {
			if param["in"] == "path" && param["name"] == name {
				found = true
				break
			}
		}
		if !found {
			params = append(params, openAPIObject{"in": "path", "name": name, "required": true, "schema": reflect.TypeFor[string]()})
		}
	}
	if len(params) != 0 {
		sort.Slice(params, func(i, j int) bool {
			return fmt.Sprint(params[i]["in"], ":", params[i]["name"]) < fmt.Sprint(params[j]["in"], ":", params[j]["name"])
		})
		for _, param := range params {
			param["schema"] = registry.schema(param["schema"].(reflect.Type))
		}
		op["parameters"] = params
	}
	if bodyType != nil {
		if bodyMedia != "" {
			media := baseMedia(bodyMedia)
			content := openAPIObject{}
			if schema, ok := registry.mediaSchema(media, bodyType); ok {
				content["schema"] = schema
			} else {
				op["x-gk-request-schema-unavailable"] = "request body schema is not statically known for this media type"
			}
			op["requestBody"] = openAPIObject{"content": openAPIObject{media: content}}
		} else {
			op["x-gk-request-schema-unavailable"] = "request body schema or media type is not statically declared"
		}
	} else if route.inputType == nil {
		op["x-gk-request-schema-unavailable"] = "request decoder does not expose a static schema"
	} else if route.inputMedia == "" && !route.sourceBinding &&
		(indirectType(route.inputType).Kind() != reflect.Struct || indirectType(route.inputType).NumField() != 0) {
		op["x-gk-request-schema-unavailable"] = "request body media type is not declared"
	}
	response := openAPIObject{"description": "Success"}
	status := route.outputStatus
	if status == 0 {
		status = 200
	}
	if route.outputHasBody && status != 204 && status != 304 && !route.negotiated && route.outputMedia != "" && route.outputType != nil {
		media := baseMedia(route.outputMedia)
		content := openAPIObject{}
		if schema, ok := registry.mediaSchema(media, responseBodyType(route.outputType)); ok {
			content["schema"] = schema
		} else {
			response["x-gk-response-schema-unavailable"] = "response body schema is not statically known for this media type"
		}
		response["content"] = openAPIObject{media: content}
	} else if route.outputHasBody && status != 204 && status != 304 {
		response["x-gk-response-schema-unavailable"] = "response media type is not statically declared"
	}
	if isReplyType(route.outputType) {
		response["x-gk-dynamic-status"] = "Reply may override the response status"
	}
	op["responses"] = openAPIObject{strconv.Itoa(status): response}
	return op
}

func isReplyType(t reflect.Type) bool {
	t = indirectType(t)
	return t != nil && t.PkgPath() == reflect.TypeFor[Reply[int]]().PkgPath() && strings.HasPrefix(t.Name(), "Reply[")
}

func responseBodyType(t reflect.Type) reflect.Type {
	if isReplyType(t) {
		field, _ := indirectType(t).FieldByName("Body")
		return field.Type
	}
	return t
}

func inputDescription(route Route) ([]openAPIObject, reflect.Type, string) {
	typ := indirectType(route.inputType)
	if typ == nil {
		return nil, nil, ""
	}
	if !route.sourceBinding {
		return nil, typ, route.inputMedia
	}
	if typ.Kind() != reflect.Struct {
		return nil, typ, route.inputMedia
	}
	var params []openAPIObject
	var body reflect.Type
	var mediaType string
	seen := make(map[reflect.Type]bool)
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		t = indirectType(t)
		if t == nil || t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		defer delete(seen, t)
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			bound := false
			for _, source := range []string{"path", "query", "header", "cookie"} {
				if tag, ok := field.Tag.Lookup(source); ok {
					bound = true
					params = append(params, openAPIObject{
						"in": source, "name": strings.Split(tag, ",")[0],
						"required": source == "path", "schema": field.Type,
					})
				}
			}
			if tag, ok := field.Tag.Lookup("body"); ok {
				body = field.Type
				switch strings.Split(tag, ",")[0] {
				case "json":
					mediaType = "application/json"
				case "xml":
					mediaType = "application/xml"
				case "form":
					mediaType = "application/x-www-form-urlencoded"
				case "text":
					mediaType = "text/plain"
				}
			} else if !bound {
				walk(field.Type)
			}
		}
	}
	walk(typ)
	if body == nil && len(params) == 0 && route.inputMedia != "" {
		return nil, typ, route.inputMedia
	}
	return params, body, mediaType
}

func pathVariableNames(path string) []string {
	var names []string
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			names = append(names, strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}"), "..."))
		}
	}
	return names
}

func baseMedia(contentType string) string {
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		return parsed
	}
	return contentType
}

func indirectType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

type schemaRegistry struct {
	names       map[reflect.Type]string
	definitions map[string]any
}

func (r *schemaRegistry) mediaSchema(media string, t reflect.Type) (openAPIObject, bool) {
	switch {
	case media == "application/json", strings.HasSuffix(media, "+json"):
		return r.schema(t), true
	case media == "application/x-www-form-urlencoded":
		return r.formSchema(t), true
	case media == "text/plain", media == "text/html":
		return openAPIObject{"type": "string"}, true
	default:
		return nil, false
	}
}

func (r *schemaRegistry) formSchema(t reflect.Type) openAPIObject {
	plan, err := compileFormPlan(t)
	if err != nil {
		return openAPIObject{}
	}
	properties := openAPIObject{}
	for _, field := range plan.fields {
		fieldType := indirectType(t)
		for _, index := range field.index {
			fieldType = indirectType(fieldType.Field(index).Type)
		}
		properties[field.name] = r.schema(fieldType)
	}
	return openAPIObject{"type": "object", "properties": properties}
}

func (r *schemaRegistry) schema(t reflect.Type) openAPIObject {
	t = indirectType(t)
	if t == nil {
		return openAPIObject{}
	}
	switch t.Kind() {
	case reflect.Bool:
		return openAPIObject{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return openAPIObject{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return openAPIObject{"type": "number"}
	case reflect.String:
		return openAPIObject{"type": "string"}
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return openAPIObject{"type": "string", "contentEncoding": "base64"}
		}
		return openAPIObject{"type": "array", "items": r.schema(t.Elem())}
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			return openAPIObject{"type": "object", "additionalProperties": r.schema(t.Elem())}
		}
		return openAPIObject{}
	case reflect.Struct:
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return openAPIObject{"type": "string", "format": "date-time"}
		}
		if t.Name() != "" {
			return r.reference(t)
		}
		return r.structSchema(t)
	default:
		return openAPIObject{}
	}
}

func (r *schemaRegistry) reference(t reflect.Type) openAPIObject {
	if name, ok := r.names[t]; ok {
		return openAPIObject{"$ref": "#/components/schemas/" + name}
	}
	name := schemaName(t)
	if _, taken := r.definitions[name]; taken {
		for i := 2; ; i++ {
			candidate := name + "_" + strconv.Itoa(i)
			if _, exists := r.definitions[candidate]; !exists {
				name = candidate
				break
			}
		}
	}
	r.names[t] = name
	r.definitions[name] = openAPIObject{}
	r.definitions[name] = r.structSchema(t)
	return openAPIObject{"$ref": "#/components/schemas/" + name}
}

func schemaName(t reflect.Type) string {
	var name strings.Builder
	for _, char := range t.PkgPath() + "." + t.Name() {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' {
			name.WriteRune(char)
		} else {
			name.WriteByte('_')
		}
	}
	return name.String()
}

func (r *schemaRegistry) structSchema(t reflect.Type) openAPIObject {
	properties := openAPIObject{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := strings.Split(field.Tag.Get("json"), ",")
		if tag[0] == "-" {
			continue
		}
		name := tag[0]
		if name == "" {
			name = field.Name
		}
		if strings.Contains(field.Tag.Get("json"), ",string") {
			properties[name] = openAPIObject{"type": "string"}
		} else {
			properties[name] = r.schema(field.Type)
		}
	}
	return openAPIObject{"type": "object", "properties": properties}
}

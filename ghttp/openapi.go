package ghttp

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

// OpenAPI builds an OpenAPI 3.1 document from route metadata.
type OpenAPI struct {
	title   string
	version string
	paths   map[string]map[string]*operation
}

type operation struct {
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	OperationID string               `json:"operationId,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
	Parameters  []*parameter         `json:"parameters,omitempty"`
	RequestBody *requestBody         `json:"requestBody,omitempty"`
	Responses   map[string]*response `json:"responses"`
}

type parameter struct {
	Name        string      `json:"name"`
	In          string      `json:"in"`
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Schema      interface{} `json:"schema"`
}

type requestBody struct {
	Required bool                  `json:"required"`
	Content  map[string]*mediaType `json:"content"`
}

type mediaType struct {
	Schema interface{} `json:"schema"`
}

type response struct {
	Description string                `json:"description"`
	Content     map[string]*mediaType `json:"content,omitempty"`
}

func NewOpenAPI(title, version string) *OpenAPI {
	return &OpenAPI{
		title:   title,
		version: version,
		paths:   make(map[string]map[string]*operation),
	}
}

func (o *OpenAPI) AddRoute(method, path, doc string, tags []string, operationID string, reqType reflect.Type, responses []responseSpec) {
	if o == nil {
		return
	}

	openapiPath := convertToOpenAPIPath(path)
	method = strings.ToLower(method)

	if _, ok := o.paths[openapiPath]; !ok {
		o.paths[openapiPath] = make(map[string]*operation)
	}

	op := &operation{
		Summary:     doc,
		Description: doc,
		OperationID: operationID,
		Tags:        tags,
	}

	if reqType != nil {
		op.Parameters = extractParameters(reqType)
	}

	if reqType != nil {
		bodySchema := extractBodySchema(reqType)
		if bodySchema != nil {
			op.RequestBody = &requestBody{
				Required: true,
				Content: map[string]*mediaType{
					"application/json": {Schema: bodySchema},
				},
			}
		}
	}

	op.Responses = make(map[string]*response)
	for _, r := range responses {
		code := strconv.Itoa(r.Code)
		respSchema := generateSchema(r.ModelType)
		op.Responses[code] = &response{
			Description: r.Description,
			Content: map[string]*mediaType{
				"application/json": {Schema: respSchema},
			},
		}
	}

	if _, ok := op.Responses["200"]; !ok && len(responses) == 0 {
		op.Responses["200"] = &response{
			Description: "OK",
		}
	}

	o.paths[openapiPath][method] = op
}

func (o *OpenAPI) Build() []byte {
	doc := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]string{
			"title":   o.title,
			"version": o.version,
		},
		"paths": o.paths,
	}

	data, _ := json.MarshalIndent(doc, "", "  ")
	return data
}

func convertToOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		} else if strings.HasPrefix(part, "*") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func extractParameters(t reflect.Type) []*parameter {
	var params []*parameter
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		switch field.Name {
		case "Path":
			params = append(params, extractFieldParams(field.Type, "path", true)...)
		case "Query":
			params = append(params, extractFieldParams(field.Type, "query", false)...)
		case "Header":
			params = append(params, extractFieldParams(field.Type, "header", false)...)
		}
	}
	return params
}

func extractFieldParams(t reflect.Type, in string, required bool) []*parameter {
	var params []*parameter
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get(in)
		if tag == "" {
			continue
		}
		param := &parameter{
			Name:     tag,
			In:       in,
			Required: required,
			Schema:   goTypeToSchemaType(f.Type.Kind()),
		}
		if doc := f.Tag.Get("doc"); doc != "" {
			param.Description = doc
		}
		params = append(params, param)
	}
	return params
}

func extractBodySchema(t reflect.Type) interface{} {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Name == "Body" {
			return generateSchema(t.Field(i).Type)
		}
	}
	return nil
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

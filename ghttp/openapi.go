package ghttp

import (
	"encoding/json"
	"reflect"
	"strings"
)

// OpenAPI builds an OpenAPI 3.1 document from route metadata.
type OpenAPI struct {
	title   string
	version string
	paths   map[string]map[string]*operation
}

type operation struct {
	Summary          string               `json:"summary,omitempty"`
	Description      string               `json:"description,omitempty"`
	OperationID      string               `json:"operationId,omitempty"`
	Tags             []string             `json:"tags,omitempty"`
	Deprecated       bool                 `json:"deprecated,omitempty"`
	Parameters       []*parameter         `json:"parameters,omitempty"`
	RequestBody      *requestBody         `json:"requestBody,omitempty"`
	Responses        map[string]*response `json:"responses"`
	ExternalDocs     *ExternalDocsDoc     `json:"externalDocs,omitempty"`
	Success          *DocMessage          `json:"x-ghttp-success,omitempty"`
	Errors           []DocMessage         `json:"x-ghttp-errors,omitempty"`
	DeprecatedReason string               `json:"x-ghttp-deprecated-reason,omitempty"`
	Sunset           string               `json:"x-ghttp-sunset,omitempty"`
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

func (o *OpenAPI) AddRoute(method, path string, reqType, respType reflect.Type, doc RouteDoc, consumes, produces []string) {
	if o == nil {
		return
	}

	openapiPath := convertToOpenAPIPath(path)
	method = strings.ToLower(method)

	if _, ok := o.paths[openapiPath]; !ok {
		o.paths[openapiPath] = make(map[string]*operation)
	}

	op := &operation{
		Summary:          doc.Summary,
		Description:      doc.Description,
		OperationID:      doc.OperationID,
		Tags:             doc.Tags,
		Deprecated:       doc.Deprecated,
		ExternalDocs:     doc.ExternalDocs,
		Success:          doc.Success,
		Errors:           doc.Errors,
		DeprecatedReason: doc.DeprecatedReason,
		Sunset:           doc.Sunset,
	}

	if reqType != nil {
		op.Parameters = append(op.Parameters, extractParametersFromType(reqType, "path", true)...)
		op.Parameters = append(op.Parameters, extractParametersFromType(reqType, "query", false)...)
		op.Parameters = append(op.Parameters, extractParametersFromType(reqType, "header", false)...)
		op.Parameters = append(op.Parameters, extractParametersFromType(reqType, "cookie", false)...)
	}

	if reqType != nil {
		bodySchema := extractBodySchema(reqType)
		if bodySchema != nil {
			contentTypes := consumes
			if len(contentTypes) == 0 {
				contentTypes = []string{MIMEJSON}
			}
			content := make(map[string]*mediaType, len(contentTypes))
			for _, contentType := range contentTypes {
				content[contentType] = &mediaType{Schema: bodySchema}
			}
			op.RequestBody = &requestBody{
				Required: true,
				Content:  content,
			}
		}
	}

	op.Responses = make(map[string]*response)
	description := "OK"
	if doc.Success != nil && doc.Success.Message != "" {
		description = doc.Success.Message
	}
	opResp := &response{Description: description}
	if respType != nil && len(produces) > 0 {
		respSchema := generateSchema(respType)
		opResp.Content = make(map[string]*mediaType, len(produces))
		for _, contentType := range produces {
			opResp.Content[contentType] = &mediaType{Schema: respSchema}
		}
	}
	op.Responses["200"] = opResp

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

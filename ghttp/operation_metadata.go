package ghttp

import (
	"errors"
	"fmt"
)

var (
	// ErrMultipleOperationBodies 表示一个输入组合声明了多个请求体。
	// ErrMultipleOperationBodies indicates that an input composition declares multiple request bodies.
	ErrMultipleOperationBodies = errors.New("multiple operation request bodies are not allowed")
	// ErrDuplicateInputParameter 表示输入组合重复声明了同一位置的同名参数。
	// ErrDuplicateInputParameter indicates that an input composition declares the same parameter twice.
	ErrDuplicateInputParameter = errors.New("duplicate operation input parameter")
	// ErrInputConstructorNil 表示自定义输入构造函数为空。
	// ErrInputConstructorNil indicates that a custom input constructor is nil.
	ErrInputConstructorNil = errors.New("input constructor is nil")
	// ErrInputMapperNil 表示输入映射函数为空。
	// ErrInputMapperNil indicates that an input mapping function is nil.
	ErrInputMapperNil = errors.New("input mapper is nil")
	// ErrInvalidInputParameter 表示输入元数据中的参数定义无效。
	// ErrInvalidInputParameter indicates that an input metadata parameter is invalid.
	ErrInvalidInputParameter = errors.New("invalid operation input parameter")
	// ErrOperationPathParameterMissing 表示输入元数据声明了不存在的路径参数。
	// ErrOperationPathParameterMissing indicates that input metadata declares a missing path parameter.
	ErrOperationPathParameterMissing = errors.New("operation path parameter is missing from route")
)

// ParameterLocation 是 OpenAPI 参数来源位置。
// ParameterLocation is an OpenAPI parameter source location.
type ParameterLocation string

const (
	// ParameterLocationPath 表示路径参数。
	// ParameterLocationPath identifies a path parameter.
	ParameterLocationPath ParameterLocation = "path"
	// ParameterLocationQuery 表示查询参数。
	// ParameterLocationQuery identifies a query parameter.
	ParameterLocationQuery ParameterLocation = "query"
	// ParameterLocationHeader 表示请求头参数。
	// ParameterLocationHeader identifies a header parameter.
	ParameterLocationHeader ParameterLocation = "header"
	// ParameterLocationCookie 表示 Cookie 参数。
	// ParameterLocationCookie identifies a cookie parameter.
	ParameterLocationCookie ParameterLocation = "cookie"
)

// InputParameter 描述输入契约中的一个 OpenAPI 参数。
// InputParameter describes one OpenAPI parameter in an input contract.
type InputParameter struct {
	Name        string
	Location    ParameterLocation
	Required    bool
	Description string
	Schema      any
}

// RequestBodyMetadata 描述输入契约中的请求体内容。
// RequestBodyMetadata describes request body content in an input contract.
type RequestBodyMetadata struct {
	Required bool
	Content  map[string]any
}

// InputMetadata 是输入描述器携带的 OpenAPI 元数据。
// InputMetadata is the OpenAPI metadata carried by an input descriptor.
type InputMetadata struct {
	Parameters  []InputParameter
	RequestBody *RequestBodyMetadata
}

func cloneInputMetadata(metadata InputMetadata) InputMetadata {
	cloned := InputMetadata{Parameters: append([]InputParameter(nil), metadata.Parameters...)}
	for index := range cloned.Parameters {
		cloned.Parameters[index].Schema = cloneOpenAPIValue(metadata.Parameters[index].Schema)
	}
	if metadata.RequestBody != nil {
		cloned.RequestBody = &RequestBodyMetadata{
			Required: metadata.RequestBody.Required,
			Content:  cloneSchemaContent(metadata.RequestBody.Content),
		}
	}
	return cloned
}

func cloneSchemaContent(content map[string]any) map[string]any {
	if content == nil {
		return nil
	}
	cloned := make(map[string]any, len(content))
	for contentType, schema := range content {
		cloned[contentType] = cloneOpenAPIValue(schema)
	}
	return cloned
}

func mergeInputMetadata(first, second InputMetadata) (InputMetadata, error) {
	merged := cloneInputMetadata(first)
	seen := make(map[string]struct{}, len(first.Parameters)+len(second.Parameters))
	for _, parameter := range merged.Parameters {
		key := string(parameter.Location) + "\x00" + parameter.Name
		if _, exists := seen[key]; exists {
			return InputMetadata{}, fmt.Errorf("%w: %s %q", ErrDuplicateInputParameter, parameter.Location, parameter.Name)
		}
		seen[key] = struct{}{}
	}
	for _, parameter := range second.Parameters {
		key := string(parameter.Location) + "\x00" + parameter.Name
		if _, exists := seen[key]; exists {
			return InputMetadata{}, fmt.Errorf("%w: %s %q", ErrDuplicateInputParameter, parameter.Location, parameter.Name)
		}
		seen[key] = struct{}{}
		parameter.Schema = cloneOpenAPIValue(parameter.Schema)
		merged.Parameters = append(merged.Parameters, parameter)
	}
	if first.RequestBody != nil && second.RequestBody != nil {
		return InputMetadata{}, ErrMultipleOperationBodies
	}
	if merged.RequestBody == nil && second.RequestBody != nil {
		merged.RequestBody = &RequestBodyMetadata{
			Required: second.RequestBody.Required,
			Content:  cloneSchemaContent(second.RequestBody.Content),
		}
	}
	return merged, nil
}

func validateOperationInputMetadata(metadata routeOpenAPIMetadata, pattern routePattern) error {
	knownPathParameters := make(map[string]struct{})
	for _, segment := range pattern.segments {
		if segment.kind == routeSegmentParameter || segment.kind == routeSegmentCatchAll {
			knownPathParameters[segment.value] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(metadata.parameters))
	for _, parameter := range metadata.parameters {
		if parameter == nil || parameter.Name == "" {
			return ErrInvalidInputParameter
		}
		switch parameter.In {
		case string(ParameterLocationPath), string(ParameterLocationQuery), string(ParameterLocationHeader), string(ParameterLocationCookie):
		default:
			return fmt.Errorf("%w: unsupported location %q", ErrInvalidInputParameter, parameter.In)
		}
		key := parameter.In + "\x00" + parameter.Name
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: %s %q", ErrDuplicateInputParameter, parameter.In, parameter.Name)
		}
		seen[key] = struct{}{}
		if parameter.In == string(ParameterLocationPath) {
			if _, exists := knownPathParameters[parameter.Name]; !exists {
				return fmt.Errorf("%w: %q", ErrOperationPathParameterMissing, parameter.Name)
			}
		}
	}
	return nil
}

func compileInputMetadata(metadata InputMetadata) routeOpenAPIMetadata {
	compiled := routeOpenAPIMetadata{}
	if len(metadata.Parameters) > 0 {
		compiled.parameters = make([]*parameter, 0, len(metadata.Parameters))
		for _, source := range metadata.Parameters {
			compiled.parameters = append(compiled.parameters, &parameter{
				Name:        source.Name,
				In:          string(source.Location),
				Description: source.Description,
				Required:    source.Required,
				Schema:      cloneOpenAPIValue(source.Schema),
			})
		}
	}
	if metadata.RequestBody != nil {
		compiled.bodyRequired = metadata.RequestBody.Required
		compiled.bodyContent = cloneSchemaContent(metadata.RequestBody.Content)
		for contentType, schema := range metadata.RequestBody.Content {
			compiled.bodyContentTypes = append(compiled.bodyContentTypes, normalizeContentType(contentType))
			if compiled.bodySchema == nil {
				compiled.bodySchema = cloneOpenAPIValue(schema)
			}
		}
	}
	return compiled
}

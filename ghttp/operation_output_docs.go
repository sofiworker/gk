package ghttp

import (
	"io"
	"net/http"
	"strconv"
)

// DocumentedHeader 描述 OpenAPI 响应头。
// DocumentedHeader describes an OpenAPI response header.
type DocumentedHeader struct {
	Description string
	Schema      any
}

// DocumentedResponse 描述一个额外 OpenAPI 响应。
// DocumentedResponse describes one additional OpenAPI response.
type DocumentedResponse struct {
	Description string
	Content     map[string]any
	Headers     map[string]DocumentedHeader
}

type outputResponsesProvider interface {
	documentedResponses() map[string]any
}

type documentedOutput[T any] struct {
	inner     Output[T]
	schema    any
	schemaSet bool
	responses map[string]any
}

func (o documentedOutput[T]) ContentType() string { return o.inner.ContentType() }
func (o documentedOutput[T]) StatusCode() int     { return o.inner.StatusCode() }
func (o documentedOutput[T]) WriteBody(writer io.Writer, value T) error {
	return o.inner.WriteBody(writer, value)
}
func (o documentedOutput[T]) prepare(value any) ([]byte, bool, error) {
	if preparer, ok := any(o.inner).(outputPreparer); ok {
		return preparer.prepare(value)
	}
	return nil, false, nil
}
func (o documentedOutput[T]) writePrepared(writer io.Writer, body []byte) error {
	if prepared, ok := any(o.inner).(preparedOutputWriter); ok {
		return prepared.writePrepared(writer, body)
	}
	return nil
}
func (o documentedOutput[T]) preflightValue(value T) error {
	if preflight, ok := any(o.inner).(outputValuePreflight[T]); ok {
		return preflight.preflightValue(value)
	}
	return nil
}
func (o documentedOutput[T]) preflightWriter(writer io.Writer) error {
	if preflight, ok := any(o.inner).(outputWriterPreflight); ok {
		return preflight.preflightWriter(writer)
	}
	return nil
}
func (o documentedOutput[T]) writeHeaders(header http.Header) {
	if headers, ok := any(o.inner).(outputHeaderWriter); ok {
		headers.writeHeaders(header)
	}
}
func (o documentedOutput[T]) writeValueHeaders(header http.Header, value T) error {
	if writer, ok := any(o.inner).(outputValueHeaderWriter[T]); ok {
		return writer.writeValueHeaders(header, value)
	}
	return nil
}
func (o documentedOutput[T]) responseHeaders() []responseHeader {
	if headers, ok := any(o.inner).(outputHeaderMetadataProvider); ok {
		return append([]responseHeader(nil), headers.responseHeaders()...)
	}
	return nil
}
func (o documentedOutput[T]) responseSchema() any {
	if o.schemaSet {
		return cloneOpenAPIValue(o.schema)
	}
	if schema, ok := any(o.inner).(outputSchemaProvider); ok {
		return schema.responseSchema()
	}
	return nil
}
func (o documentedOutput[T]) documentedResponses() map[string]any {
	responses := make(map[string]any)
	if provider, ok := any(o.inner).(outputResponsesProvider); ok {
		for status, response := range provider.documentedResponses() {
			responses[status] = cloneOpenAPIValue(response)
		}
	}
	for status, response := range o.responses {
		responses[status] = cloneOpenAPIValue(response)
	}
	return responses
}
func (o documentedOutput[T]) innerOutput() any { return o.inner }
func (o documentedOutput[T]) outputSetupError() error {
	return operationOutputSetupError(o.inner)
}

// WithOutputSchema 返回覆盖 OpenAPI 成功响应 schema 的输出契约。
// WithOutputSchema returns an output contract with an overridden OpenAPI success schema.
func WithOutputSchema[T any](schema any, output Output[T]) Output[T] {
	return documentedOutput[T]{inner: output, schema: cloneOpenAPIValue(schema), schemaSet: true}
}

// WithDocumentedResponses 返回声明额外 OpenAPI 响应的输出契约。
// WithDocumentedResponses returns an output contract declaring additional OpenAPI responses.
func WithDocumentedResponses[T any](responses map[int]DocumentedResponse, output Output[T]) Output[T] {
	compiled := make(map[string]any, len(responses))
	for status, response := range responses {
		description := response.Description
		if description == "" {
			description = http.StatusText(status)
		}
		item := map[string]any{"description": description}
		if len(response.Content) > 0 {
			content := make(map[string]any, len(response.Content))
			for contentType, schema := range response.Content {
				content[contentType] = map[string]any{"schema": cloneOpenAPIValue(schema)}
			}
			item["content"] = content
		}
		if len(response.Headers) > 0 {
			headers := make(map[string]any, len(response.Headers))
			for name, header := range response.Headers {
				headers[name] = map[string]any{
					"description": header.Description,
					"schema":      cloneOpenAPIValue(header.Schema),
				}
			}
			item["headers"] = headers
		}
		compiled[strconv.Itoa(status)] = item
	}
	return documentedOutput[T]{inner: output, responses: compiled}
}

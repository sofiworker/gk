package ghttp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// OpenAPI 返回调用时可见路由定义的尽力而为 OpenAPI 3.1 文档，不会冻结 Server。
// OpenAPI returns a best-effort OpenAPI 3.1 document; it never freezes the Server.
func (s *Server) OpenAPI() ([]byte, error) {
	if !s.config.openAPIEnabled {
		return nil, ErrOpenAPIDisabled
	}

	return compileOpenAPI(
		s.registry.snapshot(),
		s.config.openAPITitle,
		s.config.openAPIVersion,
		s.envelope != nil,
		s.config.problemDetails,
		s.config.openAPIServers,
		s.config.openAPISecurity,
	)
}

func compileOpenAPI(definitions []routeDefinition, title, version string, envelope, problemDetails bool, servers []string, security []map[string][]string) ([]byte, error) {
	paths := make(map[string]map[string]any)
	for _, definition := range definitions {
		if definition.internal {
			continue
		}

		path := openAPIPathForPattern(definition.pattern)
		pathItem := paths[path]
		if pathItem == nil {
			pathItem = make(map[string]any)
			paths[path] = pathItem
		}

		operation := openAPIOperationForDefinition(definition, envelope, problemDetails)
		if isOpenAPIPathMethod(definition.method) {
			pathItem[strings.ToLower(definition.method)] = operation
			continue
		}

		methods, _ := pathItem["x-ghttp-methods"].(map[string]any)
		if methods == nil {
			methods = make(map[string]any)
			pathItem["x-ghttp-methods"] = methods
		}
		methods[definition.method] = operation
	}

	for _, pathItem := range paths {
		get, hasGet := pathItem[strings.ToLower(http.MethodGet)]
		if !hasGet {
			continue
		}
		if _, hasHead := pathItem[strings.ToLower(http.MethodHead)]; hasHead {
			continue
		}
		pathItem[strings.ToLower(http.MethodHead)] = openAPIHeadOperation(get.(map[string]any))
	}

	document := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   title,
			"version": version,
		},
		"paths": paths,
	}
	if len(servers) > 0 {
		serverItems := make([]any, 0, len(servers))
		for _, url := range servers {
			serverItems = append(serverItems, map[string]any{"url": url})
		}
		document["servers"] = serverItems
	}
	if len(security) > 0 {
		document["security"] = security
	}
	return json.Marshal(document)
}

func isOpenAPIPathMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodTrace:
		return true
	default:
		return false
	}
}

func openAPIPathForPattern(pattern routePattern) string {
	if len(pattern.segments) == 0 {
		return "/"
	}

	segments := make([]string, len(pattern.segments))
	for index, segment := range pattern.segments {
		switch segment.kind {
		case routeSegmentParameter, routeSegmentCatchAll:
			segments[index] = "{" + segment.value + "}"
		default:
			segments[index] = openAPIStaticSegment(segment.value)
		}
	}
	path := "/" + strings.Join(segments, "/")
	if pattern.trailing {
		return path + "/"
	}
	return path
}

func openAPIStaticSegment(segment string) string {
	return url.PathEscape(segment)
}

func openAPIOperationForDefinition(definition routeDefinition, envelope, problemDetails bool) map[string]any {
	op := make(map[string]any)
	if definition.doc.Summary != "" {
		op["summary"] = definition.doc.Summary
	}
	if definition.doc.Description != "" {
		op["description"] = definition.doc.Description
	}
	if definition.doc.OperationID != "" {
		op["operationId"] = definition.doc.OperationID
	}
	if len(definition.doc.Tags) > 0 {
		op["tags"] = append([]string(nil), definition.doc.Tags...)
	}
	if definition.doc.Deprecated {
		op["deprecated"] = true
	}
	if definition.doc.ExternalDocs != nil {
		op["externalDocs"] = definition.doc.ExternalDocs
	}
	if definition.doc.Success != nil {
		op["x-ghttp-success"] = definition.doc.Success
	}
	if len(definition.doc.Errors) > 0 {
		op["x-ghttp-errors"] = append([]DocMessage(nil), definition.doc.Errors...)
	}
	if definition.doc.DeprecatedReason != "" {
		op["x-ghttp-deprecated-reason"] = definition.doc.DeprecatedReason
	}
	if definition.doc.Sunset != "" {
		op["x-ghttp-sunset"] = definition.doc.Sunset
	}

	if parameters := openAPIParametersForDefinition(definition); len(parameters) > 0 {
		op["parameters"] = parameters
	}
	if len(definition.openAPI.bodyContent) > 0 {
		content := make(map[string]any, len(definition.openAPI.bodyContent))
		for contentType, schema := range definition.openAPI.bodyContent {
			content[contentType] = map[string]any{"schema": schema}
		}
		op["requestBody"] = map[string]any{"required": definition.openAPI.bodyRequired, "content": content}
	} else if bodySchema := definition.openAPI.bodySchema; bodySchema != nil {
		contentTypes := definition.openAPI.bodyContentTypes
		if len(contentTypes) == 0 {
			contentTypes = definition.consumes
		}
		if len(contentTypes) == 0 {
			contentTypes = []string{MIMEJSON}
		}
		content := make(map[string]any, len(contentTypes))
		for _, contentType := range contentTypes {
			content[contentType] = map[string]any{"schema": bodySchema}
		}
		op["requestBody"] = map[string]any{"required": true, "content": content}
	}

	responses := openAPIResponsesForDefinition(definition, envelope, problemDetails || definition.problemDetails)
	for status, response := range definition.openAPI.responses {
		responses[status] = cloneOpenAPIValue(response)
	}
	op["responses"] = responses
	if definition.terminal == routeTerminalWebSocket {
		op["x-ghttp-websocket"] = true
	}
	return op
}

func openAPIParametersForDefinition(definition routeDefinition) []any {
	if definition.openAPI.parameters != nil {
		return openAPIParametersFromMetadata(definition.openAPI.parameters, definition.pattern)
	}
	parameters := make([]any, 0)
	known := make(map[string]struct{})
	pathParameters := make(map[string]map[string]any)
	for _, segment := range definition.pattern.segments {
		if segment.kind != routeSegmentParameter && segment.kind != routeSegmentCatchAll {
			continue
		}
		key := "path\x00" + segment.value
		if _, exists := known[key]; exists {
			if segment.kind == routeSegmentCatchAll {
				pathParameters[segment.value]["x-ghttp-catch-all"] = true
			}
			continue
		}
		parameter := map[string]any{
			"name":     segment.value,
			"in":       "path",
			"required": true,
			"schema":   map[string]any{"type": "string"},
		}
		if segment.kind == routeSegmentCatchAll {
			parameter["x-ghttp-catch-all"] = true
		}
		parameters = append(parameters, parameter)
	}
	return parameters
}

func openAPIParametersFromMetadata(metadata []*parameter, pattern routePattern) []any {
	parameters := make([]any, 0, len(metadata)+len(pattern.segments))
	known := make(map[string]struct{}, len(metadata)+len(pattern.segments))
	for _, parameter := range metadata {
		if parameter == nil {
			continue
		}
		parameters = append(parameters, openAPIParameter(parameter))
		known[parameter.In+"\x00"+parameter.Name] = struct{}{}
	}
	for _, segment := range pattern.segments {
		if segment.kind != routeSegmentParameter && segment.kind != routeSegmentCatchAll {
			continue
		}
		key := "path\x00" + segment.value
		if _, exists := known[key]; exists {
			if segment.kind == routeSegmentCatchAll {
				for _, rendered := range parameters {
					if renderedMap, ok := rendered.(map[string]any); ok && renderedMap["in"] == "path" && renderedMap["name"] == segment.value {
						renderedMap["x-ghttp-catch-all"] = true
						break
					}
				}
			}
			continue
		}
		parameter := map[string]any{
			"name": segment.value, "in": "path", "required": true,
			"schema": map[string]any{"type": "string"},
		}
		if segment.kind == routeSegmentCatchAll {
			parameter["x-ghttp-catch-all"] = true
		}
		parameters = append(parameters, parameter)
	}
	return parameters
}

func openAPIParameter(parameter *parameter) map[string]any {
	result := map[string]any{
		"name":   parameter.Name,
		"in":     parameter.In,
		"schema": parameter.Schema,
	}
	if parameter.Description != "" {
		result["description"] = parameter.Description
	}
	if parameter.Required {
		result["required"] = true
	}
	return result
}

func openAPIResponsesForDefinition(definition routeDefinition, envelope, problemDetails bool) map[string]any {
	if definition.method == http.MethodHead {
		return openAPIHeadResponses(openAPIResponsesForTerminal(definition, envelope, problemDetails))
	}
	return openAPIResponsesForTerminal(definition, envelope, problemDetails)
}

func openAPIResponsesForTerminal(definition routeDefinition, envelope, problemDetails bool) map[string]any {
	switch definition.terminal {
	case routeTerminalTyped:
		status := definition.responseStatus
		if status == 0 {
			status = http.StatusOK
		}
		description := http.StatusText(status)
		if definition.doc.Success != nil && definition.doc.Success.Message != "" {
			description = definition.doc.Success.Message
		}
		response := map[string]any{"description": description}
		if len(definition.responseHeaders) > 0 {
			headers := make(map[string]any, len(definition.responseHeaders))
			for _, header := range definition.responseHeaders {
				description := header.description
				if description == "" && header.value != "" {
					description = "Fixed response header value: " + header.value
				}
				headers[header.name] = map[string]any{
					"schema":      map[string]any{"type": "string"},
					"description": description,
				}
			}
			response["headers"] = headers
		}
		if definition.openAPI.responseSchema != nil && len(definition.produces) > 0 {
			content := make(map[string]any, len(definition.produces))
			for _, contentType := range definition.produces {
				schema := definition.openAPI.responseSchema
				if envelope {
					schema = map[string]any{
						"type": "object",
						"properties": map[string]any{
							"code": map[string]any{"type": "integer"},
							"msg":  map[string]any{"type": "string"},
							"data": schema,
						},
					}
				}
				content[contentType] = map[string]any{"schema": schema}
			}
			response["content"] = content
		}
		return map[string]any{
			strconv.Itoa(status): response,
			"404":                openAPIErrorResponse("Not Found", problemDetails),
			"405":                openAPIErrorResponse("Method Not Allowed", problemDetails),
		}
	case routeTerminalRedirect:
		status := definition.responseStatus
		if status < http.StatusMultipleChoices || status >= http.StatusBadRequest {
			return openAPIDefaultResponse()
		}
		return map[string]any{strconv.Itoa(status): map[string]any{
			"description": http.StatusText(status),
			"headers":     map[string]any{"Location": map[string]any{"schema": map[string]any{"type": "string"}}},
		}}
	case routeTerminalHTML:
		status := definition.responseStatus
		if status == 0 {
			status = http.StatusOK
		}
		return map[string]any{strconv.Itoa(status): map[string]any{
			"description": http.StatusText(status),
			"content":     map[string]any{"text/html": map[string]any{"schema": map[string]any{"type": "string"}}},
		}}
	case routeTerminalSSE:
		return map[string]any{strconv.Itoa(http.StatusOK): map[string]any{
			"description": "Server-sent event stream",
			"content":     map[string]any{"text/event-stream": map[string]any{"schema": map[string]any{"type": "string"}}},
		}}
	case routeTerminalWebSocket:
		return map[string]any{strconv.Itoa(http.StatusSwitchingProtocols): map[string]any{"description": "WebSocket upgrade"}}
	default:
		return openAPIDefaultResponse()
	}
}

func openAPIDefaultResponse() map[string]any {
	return map[string]any{"default": map[string]any{"description": "Response written by handler"}}
}

func openAPIErrorResponse(description string, problemDetails bool) map[string]any {
	if problemDetails {
		return map[string]any{
			"description": description,
			"content": map[string]any{
				MIMEProblemJSON: map[string]any{
					"schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type":     map[string]any{"type": "string", "format": "uri-reference"},
							"title":    map[string]any{"type": "string"},
							"status":   map[string]any{"type": "integer"},
							"detail":   map[string]any{"type": "string"},
							"instance": map[string]any{"type": "string", "format": "uri-reference"},
						},
					},
				},
			},
		}
	}
	return map[string]any{
		"description": description,
		"content": map[string]any{
			MIMEJSON: map[string]any{
				"schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"code":    map[string]any{"type": "integer"},
						"message": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

func openAPIHeadOperation(operation map[string]any) map[string]any {
	cloned := cloneOpenAPIValue(operation).(map[string]any)
	cloned["responses"] = openAPIHeadResponses(cloned["responses"].(map[string]any))
	return cloned
}

func openAPIHeadResponses(responses map[string]any) map[string]any {
	cloned := cloneOpenAPIValue(responses).(map[string]any)
	for _, value := range cloned {
		response, ok := value.(map[string]any)
		if !ok {
			continue
		}
		delete(response, "content")
	}
	return cloned
}

func cloneOpenAPIValue(value any) any {
	encoded, _ := json.Marshal(value)
	var cloned any
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}

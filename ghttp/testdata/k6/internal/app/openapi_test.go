package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIEndpointAndOutputOperations(t *testing.T) {
	document := openAPIDocument(t, Config{})
	paths := openAPIMapKey(t, document, "$.paths", "paths")
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	typed := []struct {
		path, method, status, contentType string
		runtimePath                       string
		runtimeStatus                     int
	}{
		{path: "/output/json", method: "get", status: "200", contentType: "application/json", runtimeStatus: 200},
		{path: "/output/xml", method: "get", status: "200", contentType: "application/xml", runtimeStatus: 200},
		{path: "/output/text", method: "get", status: "200", contentType: "text/plain", runtimeStatus: 200},
		{path: "/output/created", method: "post", status: "201", runtimeStatus: 201},
		{path: "/output/accepted", method: "post", status: "202", runtimeStatus: 202},
		{path: "/output/empty", method: "delete", status: "204", runtimeStatus: 204},
		{path: "/output/redirect", method: "get", status: "307", runtimeStatus: 307},
		{path: "/binding/path/{id}", runtimePath: "/binding/path/1", method: "get", status: "200", contentType: "application/json", runtimeStatus: 200},
	}
	for _, item := range typed {
		response := openAPIResponse(t, paths, item.path, item.method, item.status)
		if item.contentType != "" {
			content := openAPIMapKey(t, response, openAPIPath(item.path, item.method, "responses", item.status, "content"), "content")
			openAPIMapKey(t, content, openAPIPath(item.path, item.method, "responses", item.status, "content", item.contentType), item.contentType)
		}
		runtimePath := item.runtimePath
		if runtimePath == "" {
			runtimePath = item.path
		}
		method := http.MethodGet
		if item.method == "post" {
			method = http.MethodPost
		} else if item.method == "delete" {
			method = http.MethodDelete
		}
		recorder := request(t, handler, method, runtimePath)
		if recorder.Code != item.runtimeStatus {
			t.Fatalf("runtime %s %s = %d, doc status %s", method, runtimePath, recorder.Code, item.status)
		}
		if item.contentType != "" && recorder.Header().Get("Content-Type") != item.contentType {
			t.Fatalf("runtime %s %s Content-Type = %q, doc content %q", method, runtimePath, recorder.Header().Get("Content-Type"), item.contentType)
		}
	}

	for _, item := range []struct{ path, method string }{
		{path: "/output/binary", method: "get"},
		{path: "/output/headers", method: "get"},
		{path: "/files/sample", method: "get"},
		{path: "/files/range", method: "get"},
		{path: "/files/etag", method: "get"},
	} {
		responses := openAPIResponses(t, paths, item.path, item.method)
		openAPIMapKey(t, responses, openAPIPath(item.path, item.method, "responses", "default"), "default")
		for _, forbidden := range []string{"200", "206", "304", "416"} {
			if _, exists := responses[forbidden]; exists {
				t.Fatalf("%s contains fabricated response %s: %#v", openAPIPath(item.path, item.method, "responses"), forbidden, responses)
			}
		}
	}

	created := openAPIResponse(t, paths, "/output/created", "post", "201")
	if _, exists := created["content"]; exists {
		t.Fatalf("%s unexpectedly contains content", openAPIPath("/output/created", "post", "responses", "201"))
	}
	head := openAPIResponse(t, paths, "/output/json", "head", "200")
	if _, exists := head["content"]; exists {
		t.Fatalf("%s unexpectedly contains content", openAPIPath("/output/json", "head", "responses", "200"))
	}
	errorResponse := openAPIResponse(t, paths, "/output/json", "get", "404")
	errorContent := openAPIMapKey(t, errorResponse, "$.paths[/output/json].get.responses[404].content", "content")
	errorMedia := openAPIMapKey(t, errorContent, "$.paths[/output/json].get.responses[404].content[application/json]", "application/json")
	errorSchema := openAPIMapKey(t, errorMedia, "$.paths[/output/json].get.responses[404].content[application/json].schema", "schema")
	if got := errorSchema["type"]; got != "object" {
		t.Fatalf("$.paths[/output/json].get.responses[404].content[application/json].schema.type = %#v, want object", got)
	}
	errorProperties := openAPIMapKey(t, errorSchema, "$.paths[/output/json].get.responses[404].content[application/json].schema.properties", "properties")
	openAPIMapKey(t, errorProperties, "$.paths[/output/json].get.responses[404].content[application/json].schema.properties.code", "code")
	openAPIMapKey(t, errorProperties, "$.paths[/output/json].get.responses[404].content[application/json].schema.properties.message", "message")

	requestContent := openAPIMapAt(t, document, "$.paths[/codec/negotiate].post.requestBody.content", "paths", "/codec/negotiate", "post", "requestBody", "content")
	jsonMedia := openAPIMapKey(t, requestContent, "$.paths[/codec/negotiate].post.requestBody.content[application/json]", "application/json")
	openAPIMapKey(t, jsonMedia, "$.paths[/codec/negotiate].post.requestBody.content[application/json].schema", "schema")
}

func TestOpenAPISchemasAndRuntimeStatusBoundary(t *testing.T) {
	document := openAPIDocument(t, Config{MaxBodyBytes: 8})
	paths := openAPIMapKey(t, document, "$.paths", "paths")
	parameters := openAPISliceAt(t, document, "$.paths[/binding/path/{id}].get.parameters", "paths", "/binding/path/{id}", "get", "parameters")
	pathParameter := openAPIMapIndex(t, parameters, "$.paths[/binding/path/{id}].get.parameters[0]", 0)
	if required, ok := pathParameter["required"].(bool); !ok || !required {
		t.Fatalf("$.paths[/binding/path/{id}].get.parameters[0].required = %#v, want true", pathParameter["required"])
	}

	bodySchema := openAPIMapAt(t, document, "$.paths[/output/json].get.responses[200].content[application/json].schema", "paths", "/output/json", "get", "responses", "200", "content", "application/json", "schema")
	if got := bodySchema["type"]; got != "object" {
		t.Fatalf("JSON output schema type = %#v, want object", got)
	}
	properties := openAPIMapKey(t, bodySchema, "$.paths[/output/json].get.responses[200].content[application/json].schema.properties", "properties")
	if got := openAPIMapKey(t, properties, "$.paths[/output/json].get.responses[200].content[application/json].schema.properties.tags", "tags")["type"]; got != "array" {
		t.Fatalf("tags type = %#v, want array", got)
	}
	if got := openAPIMapKey(t, properties, "$.paths[/output/json].get.responses[200].content[application/json].schema.properties.meta", "meta")["type"]; got != "object" {
		t.Fatalf("meta type = %#v, want object", got)
	}
	required := openAPISliceKey(t, bodySchema, "$.paths[/output/json].get.responses[200].content[application/json].schema.required", "required")
	if len(required) != 1 || required[0] != "message" {
		t.Fatalf("required = %#v, want [message]", required)
	}
	if containsOpenAPINullable(document) {
		t.Fatal("OpenAPI unexpectedly claims nullable support that ghttp schema generation does not expose")
	}

	handler, cleanup := New(Config{MaxBodyBytes: 8})
	t.Cleanup(cleanup)
	checks := []struct {
		name, method, path, body, contentType string
		headers                               map[string]string
		status                                int
		docPath, docMethod                    string
		docDeclares                           bool
	}{
		{name: "bad request", method: http.MethodGet, path: "/binding/path/not-an-int", status: 400, contentType: "application/problem+json", docPath: "/binding/path/{id}", docMethod: "get"},
		{name: "typed operation not found", method: http.MethodGet, path: "/output/json?missing=true", status: 404, contentType: "application/json", docPath: "/output/json", docMethod: "get", docDeclares: true},
		{name: "not acceptable", method: http.MethodPost, path: "/codec/negotiate", body: `{"name":"x"}`, headers: map[string]string{"Content-Type": "application/json", "Accept": "text/html"}, status: 406, contentType: "application/problem+json", docPath: "/codec/negotiate", docMethod: "post"},
		{name: "too large", method: http.MethodPost, path: "/codec/body/limited", body: `{"data":"too-large"}`, headers: map[string]string{"Content-Type": "application/json"}, status: 413, contentType: "application/problem+json", docPath: "/codec/body/limited", docMethod: "post"},
		{name: "unsupported media type", method: http.MethodPost, path: "/codec/xml", body: `{"name":"x"}`, headers: map[string]string{"Content-Type": "application/json"}, status: 415, contentType: "application/problem+json", docPath: "/codec/xml", docMethod: "post"},
		{name: "unprocessable entity", method: http.MethodGet, path: "/binding/path/0", status: 422, contentType: "application/problem+json", docPath: "/binding/path/{id}", docMethod: "get"},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(check.method, check.path, bytes.NewBufferString(check.body))
			for name, value := range check.headers {
				req.Header.Set(name, value)
			}
			handler.ServeHTTP(recorder, req)
			if recorder.Code != check.status || recorder.Header().Get("Content-Type") != check.contentType {
				t.Fatalf("runtime = %d %q, want %d %q", recorder.Code, recorder.Header().Get("Content-Type"), check.status, check.contentType)
			}
			if check.docPath != "" {
				responses := openAPIResponses(t, paths, check.docPath, check.docMethod)
				_, exists := responses[fmt.Sprint(check.status)]
				if exists != check.docDeclares {
					t.Fatalf("%s status %d declared=%v, want %v", openAPIPath(check.docPath, check.docMethod, "responses"), check.status, exists, check.docDeclares)
				}
			}
		})
	}
}

func openAPIDocument(t *testing.T, cfg Config) map[string]any {
	t.Helper()
	handler, cleanup := New(cfg)
	t.Cleanup(cleanup)
	recorder := request(t, handler, http.MethodGet, "/openapi.json")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET /openapi.json = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	var document map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("OpenAPI JSON error = %v", err)
	}
	return document
}

func openAPIResponse(t *testing.T, paths map[string]any, path, method, status string) map[string]any {
	t.Helper()
	responses := openAPIResponses(t, paths, path, method)
	return openAPIMapKey(t, responses, openAPIPath(path, method, "responses", status), status)
}

func openAPIResponses(t *testing.T, paths map[string]any, path, method string) map[string]any {
	t.Helper()
	pathItem := openAPIMapKey(t, paths, "$.paths["+path+"]", path)
	operation := openAPIMapKey(t, pathItem, openAPIPath(path, method), method)
	return openAPIMapKey(t, operation, openAPIPath(path, method, "responses"), "responses")
}

func openAPIMapAt(t *testing.T, root map[string]any, jsonPath string, keys ...string) map[string]any {
	t.Helper()
	current := root
	path := "$"
	for _, key := range keys {
		path += "[" + key + "]"
		current = openAPIMapKey(t, current, path, key)
	}
	return current
}

func openAPISliceAt(t *testing.T, root map[string]any, jsonPath string, keys ...string) []any {
	t.Helper()
	if len(keys) == 0 {
		t.Fatalf("%s: missing slice key", jsonPath)
	}
	parent := openAPIMapAt(t, root, jsonPath, keys[:len(keys)-1]...)
	return openAPISliceKey(t, parent, jsonPath, keys[len(keys)-1])
}

func openAPIMapKey(t *testing.T, parent map[string]any, jsonPath, key string) map[string]any {
	t.Helper()
	value, exists := parent[key]
	if !exists {
		t.Fatalf("%s: missing key %q", jsonPath, key)
	}
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s: got %T, want object", jsonPath, value)
	}
	return result
}

func openAPISliceKey(t *testing.T, parent map[string]any, jsonPath, key string) []any {
	t.Helper()
	value, exists := parent[key]
	if !exists {
		t.Fatalf("%s: missing key %q", jsonPath, key)
	}
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("%s: got %T, want array", jsonPath, value)
	}
	return result
}

func openAPIMapIndex(t *testing.T, values []any, jsonPath string, index int) map[string]any {
	t.Helper()
	if index < 0 || index >= len(values) {
		t.Fatalf("%s: index %d outside length %d", jsonPath, index, len(values))
	}
	result, ok := values[index].(map[string]any)
	if !ok {
		t.Fatalf("%s: got %T, want object", jsonPath, values[index])
	}
	return result
}

func openAPIPath(path, method string, suffix ...string) string {
	result := "$.paths[" + path + "]." + method
	for _, part := range suffix {
		result += "[" + part + "]"
	}
	return result
}

func containsOpenAPINullable(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		if value["nullable"] == true {
			return true
		}
		for _, child := range value {
			if containsOpenAPINullable(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if child == "null" || containsOpenAPINullable(child) {
				return true
			}
		}
	}
	return false
}

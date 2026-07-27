package ghttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSONErrorRenderer(t *testing.T) {
	document := ErrorDocument{Status: 404, MessageID: "user.not_found", Args: map[string]any{"user_id": 42}, Message: "用户不存在"}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	if err := JSONErrorRenderer().RenderError(recorder, request, document); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != 404 || recorder.Header().Get("Content-Type") != MIMEJSON {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	var got ErrorDocument
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.MessageID != document.MessageID || got.Message != document.Message || got.Args == nil {
		t.Fatalf("body = %#v", got)
	}
}

func TestProblemJSONRenderer(t *testing.T) {
	document := ErrorDocument{Status: 422, MessageID: "request.validation_failed", Args: map[string]any{}, Details: []ErrorDetail{{Location: "body.email", MessageID: "validation.email", Args: map[string]any{}}}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := ProblemJSONRenderer().RenderError(recorder, request, document); err != nil {
		t.Fatal(err)
	}
	if recorder.Header().Get("Content-Type") != MIMEProblemJSON {
		t.Fatalf("content type = %q", recorder.Header().Get("Content-Type"))
	}
	var got map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "urn:ghttp:error:request.validation_failed" || got["title"] != document.MessageID || got["status"] != float64(422) {
		t.Fatalf("problem = %#v", got)
	}
	if _, ok := got["detail"]; ok {
		t.Fatal("detail must be omitted without localized message")
	}
}

func TestErrorRendererOpenAPIDescriptors(t *testing.T) {
	jsonDescriptor := JSONErrorRenderer().OpenAPIDescriptor()
	problemDescriptor := ProblemJSONRenderer().OpenAPIDescriptor()
	if jsonDescriptor.ContentType != MIMEJSON || jsonDescriptor.ComponentName == "" || jsonDescriptor.Schema == nil {
		t.Fatalf("json descriptor = %#v", jsonDescriptor)
	}
	if problemDescriptor.ContentType != MIMEProblemJSON || problemDescriptor.ComponentName == jsonDescriptor.ComponentName {
		t.Fatalf("problem descriptor = %#v", problemDescriptor)
	}
}

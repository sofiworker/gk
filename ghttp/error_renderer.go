package ghttp

import (
	"encoding/json"
	"net/http"
)

const MIMEProblemJSON = "application/problem+json"

type ErrorOpenAPIDescriptor struct {
	ContentType   string
	ComponentName string
	Schema        map[string]any
	Headers       map[string]map[string]any
}

type ErrorRenderer interface {
	RenderError(http.ResponseWriter, *http.Request, ErrorDocument) error
	OpenAPIDescriptor() ErrorOpenAPIDescriptor
}

type jsonErrorRenderer struct{}

func JSONErrorRenderer() ErrorRenderer { return jsonErrorRenderer{} }

func (jsonErrorRenderer) RenderError(w http.ResponseWriter, r *http.Request, document ErrorDocument) error {
	document = cloneErrorDocument(document)
	if document.Args == nil {
		document.Args = map[string]any{}
	}
	w.Header().Set("Content-Type", MIMEJSON)
	w.WriteHeader(document.Status)
	if r != nil && r.Method == http.MethodHead {
		return nil
	}
	return json.NewEncoder(w).Encode(document)
}

func (jsonErrorRenderer) OpenAPIDescriptor() ErrorOpenAPIDescriptor {
	return ErrorOpenAPIDescriptor{ContentType: MIMEJSON, ComponentName: "GHTTPError", Schema: errorDocumentSchema()}
}

type problemJSONRenderer struct{}

func ProblemJSONRenderer() ErrorRenderer { return problemJSONRenderer{} }

type problemDocument struct {
	Type      string         `json:"type"`
	Status    int            `json:"status"`
	Title     string         `json:"title"`
	Detail    string         `json:"detail,omitempty"`
	MessageID string         `json:"message_id"`
	Args      map[string]any `json:"args"`
	Errors    []ErrorDetail  `json:"errors,omitempty"`
}

func (problemJSONRenderer) RenderError(w http.ResponseWriter, r *http.Request, document ErrorDocument) error {
	document = cloneErrorDocument(document)
	if document.Args == nil {
		document.Args = map[string]any{}
	}
	problem := problemDocument{
		Type: "urn:ghttp:error:" + document.MessageID, Status: document.Status,
		Title: document.MessageID, Detail: document.Message, MessageID: document.MessageID,
		Args: document.Args, Errors: document.Details,
	}
	w.Header().Set("Content-Type", MIMEProblemJSON)
	w.WriteHeader(document.Status)
	if r != nil && r.Method == http.MethodHead {
		return nil
	}
	return json.NewEncoder(w).Encode(problem)
}

func (problemJSONRenderer) OpenAPIDescriptor() ErrorOpenAPIDescriptor {
	schema := errorDocumentSchema()
	properties := schema["properties"].(map[string]any)
	properties["type"] = map[string]any{"type": "string"}
	properties["title"] = map[string]any{"type": "string"}
	properties["detail"] = map[string]any{"type": "string"}
	return ErrorOpenAPIDescriptor{ContentType: MIMEProblemJSON, ComponentName: "GHTTPProblem", Schema: schema}
}

func errorDocumentSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"status", "message_id", "args"},
		"properties": map[string]any{
			"status":     map[string]any{"type": "integer", "minimum": 400, "maximum": 599},
			"message_id": map[string]any{"type": "string"},
			"args":       map[string]any{"type": "object", "additionalProperties": true},
			"message":    map[string]any{"type": "string"},
			"errors":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		},
	}
}

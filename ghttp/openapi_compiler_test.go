package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestServerOpenAPISnapshotDoesNotFreezeRoutes(t *testing.T) {
	t.Parallel()

	server := New(WithOpenAPI("example", "1.0.0"))
	Route[struct{}, struct{}](server).
		GET("/health").
		ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	if server.routed {
		t.Fatal("OpenAPI must not freeze the server")
	}

	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("OpenAPI JSON error = %v", err)
	}
	if got := decoded["openapi"]; got != "3.1.0" {
		t.Fatalf("openapi = %#v, want 3.1.0", got)
	}
	paths := decoded["paths"].(map[string]any)
	if _, ok := paths["/health"]; !ok {
		t.Fatalf("paths = %#v, want /health", paths)
	}

	Route[struct{}, struct{}](server).
		GET("/after-snapshot").
		ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
}

func TestServerOpenAPIDisabled(t *testing.T) {
	t.Parallel()

	_, err := New().OpenAPI()
	if !errors.Is(err, ErrOpenAPIDisabled) {
		t.Fatalf("OpenAPI error = %v, want ErrOpenAPIDisabled", err)
	}
}

func TestServerOpenAPIWritesCustomMethodsAsExtension(t *testing.T) {
	t.Parallel()

	server := New(WithOpenAPI("example", "1.0.0"))
	Route[struct{}, struct{}](server).
		CONNECT("/tunnel").
		ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	Route[struct{}, struct{}](server).
		CUSTOM("purge", "/cache").
		ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("OpenAPI JSON error = %v", err)
	}
	paths := decoded["paths"].(map[string]any)
	for path, method := range map[string]string{"/tunnel": http.MethodConnect, "/cache": "purge"} {
		item := paths[path].(map[string]any)
		if _, exists := item[strings.ToLower(method)]; exists {
			t.Fatalf("path item contains invalid method key %q: %#v", strings.ToLower(method), item)
		}
		methods := item["x-ghttp-methods"].(map[string]any)
		if _, exists := methods[method]; !exists {
			t.Fatalf("x-ghttp-methods = %#v, want %q", methods, method)
		}
	}
}

func TestServerOpenAPIDescribesRawCatchAllRouteBestEffort(t *testing.T) {
	t.Parallel()

	server := New(WithOpenAPI("example", "1.0.0"))
	Route[struct{}, struct{}](server).
		GET("/files/{path...}").
		ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("OpenAPI JSON error = %v", err)
	}
	paths := decoded["paths"].(map[string]any)
	item, ok := paths["/files/{path}"].(map[string]any)
	if !ok {
		t.Fatalf("paths = %#v, want /files/{path}", paths)
	}
	operation := item["get"].(map[string]any)
	responses := operation["responses"].(map[string]any)
	if _, exists := responses["200"]; exists {
		t.Fatalf("raw responses = %#v, must not declare 200", responses)
	}
	if _, exists := responses["default"]; !exists {
		t.Fatalf("raw responses = %#v, want default", responses)
	}
	parameters := operation["parameters"].([]any)
	if len(parameters) != 1 {
		t.Fatalf("parameters = %#v, want one catch-all parameter", parameters)
	}
	parameter := parameters[0].(map[string]any)
	if parameter["name"] != "path" || parameter["x-ghttp-catch-all"] != true {
		t.Fatalf("catch-all parameter = %#v", parameter)
	}
}

func TestServerOpenAPIMarksTypedCatchAllParameter(t *testing.T) {
	t.Parallel()

	type request struct {
		Path string `path:"path"`
	}
	server := New(WithOpenAPI("example", "1.0.0"), WithProduces(MIMEJSON))
	Route[request, struct{}](server).
		GET("/files/{path...}").
		To(func(context.Context, request) (struct{}, error) {
			return struct{}{}, nil
		})

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("OpenAPI JSON error = %v", err)
	}
	operation := decoded["paths"].(map[string]any)["/files/{path}"].(map[string]any)["get"].(map[string]any)
	parameters := operation["parameters"].([]any)
	if len(parameters) != 1 {
		t.Fatalf("parameters = %#v, want one parameter", parameters)
	}
	parameter := parameters[0].(map[string]any)
	if parameter["x-ghttp-catch-all"] != true {
		t.Fatalf("catch-all parameter = %#v, want x-ghttp-catch-all", parameter)
	}
}

func TestServerOpenAPIEscapesStaticBraces(t *testing.T) {
	t.Parallel()

	server := New(WithOpenAPI("example", "1.0.0"))
	Route[struct{}, struct{}](server).
		GET("/%7Bid%7D").
		ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("OpenAPI JSON error = %v", err)
	}
	paths := decoded["paths"].(map[string]any)
	if _, exists := paths["/%7Bid%7D"]; !exists {
		t.Fatalf("paths = %#v, want escaped static braces", paths)
	}
	if _, exists := paths["/{id}"]; exists {
		t.Fatalf("paths = %#v, must not contain a template parameter", paths)
	}
}

func TestServerOpenAPIEscapesStaticQueryAndFragmentDelimiters(t *testing.T) {
	t.Parallel()

	server := New(WithOpenAPI("example", "1.0.0"))
	Route[struct{}, struct{}](server).
		GET("/%3F%23").
		ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("OpenAPI JSON error = %v", err)
	}
	paths := decoded["paths"].(map[string]any)
	if _, exists := paths["/%3F%23"]; !exists {
		t.Fatalf("paths = %#v, want escaped query and fragment delimiters", paths)
	}
}

func TestServerOpenAPIDerivesContentFreeHEADFallback(t *testing.T) {
	t.Parallel()

	server := New(WithOpenAPI("example", "1.0.0"), WithProduces(MIMEJSON))
	Route[struct{}, struct {
		Name string `json:"name"`
	}](server).
		GET("/users").
		To(func(context.Context, struct{}) (struct {
			Name string `json:"name"`
		}, error) {
			return struct {
				Name string `json:"name"`
			}{}, nil
		})

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("OpenAPI JSON error = %v", err)
	}
	item := decoded["paths"].(map[string]any)["/users"].(map[string]any)
	head, ok := item["head"].(map[string]any)
	if !ok {
		t.Fatalf("path item = %#v, want derived head operation", item)
	}
	response := head["responses"].(map[string]any)["200"].(map[string]any)
	if _, exists := response["content"]; exists {
		t.Fatalf("HEAD response = %#v, must omit content", response)
	}
}

func TestServerRejectsAnyUserMethodOnOpenAPIEndpointPath(t *testing.T) {
	t.Parallel()

	server := New(WithOpenAPI("example", "1.0.0"))
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("POST on the OpenAPI endpoint path did not panic")
		} else if err, ok := recovered.(error); !ok || !errors.Is(err, ErrRouteConflict) {
			t.Fatalf("panic = %#v, want ErrRouteConflict", recovered)
		}
	}()
	Route[struct{}, struct{}](server).
		POST("/openapi.json").
		ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
}

type openAPIResp struct {
	ID string `json:"id"`
}

func (r openAPIResp) StatusCode() int { return http.StatusCreated }

func TestOpenAPIEnvelopeErrorResponsesAndServers(t *testing.T) {
	app := New(
		WithProduces(MIMEJSON),
		WithOpenAPI("t", "v1"),
		WithEnvelope(DefaultEnvelope),
		WithOpenAPIServers("https://api.example.com"),
		WithOpenAPISecurity(map[string][]string{"apiKey": {}}),
	)
	Route[Params, openAPIResp](app).POST("/users").Status(http.StatusCreated).To(func(context.Context, Params) (openAPIResp, error) {
		return openAPIResp{ID: "u-1"}, nil
	})

	doc, err := app.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	text := string(doc)
	if !strings.Contains(text, `"201"`) {
		t.Fatalf("missing 201 response: %s", text)
	}
	if !strings.Contains(text, `"data"`) || !strings.Contains(text, `"code"`) {
		t.Fatalf("envelope schema missing: %s", text)
	}
	if !strings.Contains(text, `"404"`) || !strings.Contains(text, `"405"`) {
		t.Fatalf("error responses missing: %s", text)
	}
	if !strings.Contains(text, `"servers"`) || !strings.Contains(text, `"https://api.example.com"`) {
		t.Fatalf("servers missing: %s", text)
	}
	if !strings.Contains(text, `"security"`) || !strings.Contains(text, `"apiKey"`) {
		t.Fatalf("security missing: %s", text)
	}
}

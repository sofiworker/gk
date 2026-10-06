package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWSHandlerAndMiddleware(t *testing.T) {
	s := NewServer()
	var order []string
	middleware := func(label string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				order = append(order, label)
				return next(ctx, req, resp)
			}
		}
	}
	s.Use(middleware("server"))
	group := s.Group("/chat").Use(middleware("group"))
	if err := group.Register(WS("/:room", func(_ context.Context, req *Request, resp *Response) error {
		order = append(order, "handler")
		if req.Params.Get("room") != "lobby" {
			t.Fatal("path parameter was not passed to handler")
		}
		// The selected library owns rejection of invalid upgrade requests.
		resp.WriteHeader(http.StatusBadRequest)
		_, err := io.WriteString(resp, "library handshake error")
		return err
	}, WithMiddleware(middleware("route")))); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/chat/lobby", nil))
	if rec.Code != http.StatusBadRequest || rec.Body.String() != "library handshake error" {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
	if got := strings.Join(order, ","); got != "server,group,route,handler" {
		t.Fatalf("execution order = %s", got)
	}

	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/chat/lobby", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
		t.Fatalf("method rejection = %d, Allow = %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestWSRegistrationValidation(t *testing.T) {
	handler := RawHandlerFunc(func(context.Context, *Request, *Response) error { return nil })
	for _, tt := range []struct {
		name  string
		route Route
	}{
		{"nil handler", WS("/ws", nil)},
		{"empty path", WS("", handler)},
		{"input", WS("/ws", handler, WithInput(JSONInput[NoDataType]()))},
		{"output", WS("/ws", handler, WithOutput(TextOutput()))},
		{"nil option", WS("/ws", handler, nil)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := NewServer().Register(tt.route); err == nil {
				t.Fatal("registration succeeded with invalid arguments")
			}
		})
	}
}

func TestWSMetadataAndOpenAPI(t *testing.T) {
	s := NewServer()
	handler := RawHandlerFunc(func(context.Context, *Request, *Response) error { return nil })
	if err := s.Register(
		WS("/ws", handler, WithDoc("Chat", "Application-owned handshake")),
		WS("/private-ws", handler, WithOpenAPIHidden()),
	); err != nil {
		t.Fatal(err)
	}
	for _, route := range s.Routes() {
		if route.Kind != RouteWebSocket || route.BodyType != nil || route.ResultType != nil {
			t.Fatalf("unexpected route metadata: %+v", route)
		}
	}
	doc, err := OpenAPIDocument(s)
	if err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	if _, exists := paths["/private-ws"]; exists {
		t.Fatal("hidden WebSocket route appeared in OpenAPI")
	}
	op := paths["/ws"].(map[string]any)["get"].(map[string]any)
	responses := op["responses"].(map[string]any)
	if _, ok := responses["101"]; !ok {
		t.Fatal("missing upgrade response")
	}
	if _, ok := responses["204"]; ok {
		t.Fatal("WebSocket route described as No Content")
	}
}

func TestWSBodyLimit(t *testing.T) {
	s := NewServer()
	if err := s.Register(WS("/ws", func(_ context.Context, req *Request, _ *Response) error {
		_, err := io.ReadAll(req.Raw.Body)
		return err
	}, WithBodyLimit(4))); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", strings.NewReader("oversized")))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

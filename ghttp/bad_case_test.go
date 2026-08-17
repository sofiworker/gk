package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type badCaseInput struct {
	Params `json:"-"`

	Body struct {
		Name string `json:"name"`
	}
}

type badCaseOutput struct {
	ID      string `json:"id"`
	Page    string `json:"page"`
	Flag    string `json:"flag"`
	TraceID string `json:"trace_id"`
	Name    string `json:"name"`
}

func TestOperationBadCasesMalformedJSONReturnsBadRequest(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/users/{id}"), StructInput[badCaseInput](), JSONOutput[badCaseOutput](), badCaseEchoHandler))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/42", strings.NewReader(`{"name":`))
	req.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestOperationBadCasesParamsKeepRawScalarValues(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/users/{id}"), StructInput[badCaseInput](), JSONOutput[badCaseOutput](), badCaseEchoHandler))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/not-int?page=bad&flag=maybe", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", "trace-1")
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := decodeJSONBody[badCaseOutput](t, rec.Body.Bytes())
	if got.ID != "not-int" || got.Page != "bad" || got.Flag != "maybe" {
		t.Fatalf("raw values = id:%q page:%q flag:%q", got.ID, got.Page, got.Flag)
	}
	if got.TraceID != "trace-1" || got.Name != "alice" {
		t.Fatalf("preserved values = trace:%q name:%q, want trace-1/alice", got.TraceID, got.Name)
	}
}

func TestOperationBadCasesValidatorErrorReturnsUnprocessableEntity(t *testing.T) {
	wantErr := errors.New("validation failed")
	app := New(WithValidator(serverValidatorFunc(func(context.Context, interface{}) error {
		return wantErr
	})), WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/users/{id}"), StructInput[badCaseInput](), JSONOutput[badCaseOutput](), badCaseEchoHandler))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/42", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestOperationBadCasesHandlerHTTPErrorStatusIsPreserved(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/conflict"), StructInput[struct{}](), JSONOutput[struct{}](), func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, Conflict("already exists")
	}))

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/conflict", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestOperationBadCasesMiddlewareCanShortCircuitRoute(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/blocked"), StructInput[struct{}](), JSONOutput[struct{}](), func(context.Context, struct{}) (struct{}, error) {
		t.Fatal("handler should not be called")
		return struct{}{}, nil
	}).WithMiddleware(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte("blocked"))
		})
	}))

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blocked", nil))

	if rec.Code != http.StatusTeapot || rec.Body.String() != "blocked" {
		t.Fatalf("response = %d %q, want 418 blocked", rec.Code, rec.Body.String())
	}
}

func TestOperationBadCasesGroupPathJoiningEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		routePath string
		wantPath  string
	}{
		{name: "empty prefix", prefix: "", routePath: "/users", wantPath: "/users"},
		{name: "root prefix", prefix: "/", routePath: "/users", wantPath: "/users"},
		{name: "trailing prefix slash", prefix: "/api/", routePath: "/users", wantPath: "/api/users"},
		{name: "relative route path", prefix: "/api", routePath: "users", wantPath: "/api/users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(WithProduces(MIMEJSON))
			group := app.Group(tt.prefix)
			group.MustMount(Handle(Get(tt.routePath), StructInput[struct{}](), JSONOutput[badCaseOutput](), func(context.Context, struct{}) (badCaseOutput, error) {
				return badCaseOutput{Name: tt.name}, nil
			}))

			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.wantPath, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d", tt.wantPath, rec.Code, http.StatusOK)
			}
		})
	}
}

func TestOperationBadCasesCORSPreflightShortCircuitsOptionsRoute(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(CORS(CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodOptions},
	}))
	app.MustMount(Handle(Options("/options"), StructInput[struct{}](), JSONOutput[badCaseOutput](), func(context.Context, struct{}) (badCaseOutput, error) {
		t.Fatal("OPTIONS handler should not be called for CORS preflight")
		return badCaseOutput{}, nil
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/options", nil)
	req.Header.Set("Origin", "https://example.test")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty preflight response", rec.Body.String())
	}
}

func badCaseEchoHandler(ctx context.Context, req badCaseInput) (badCaseOutput, error) {
	return badCaseOutput{
		ID:      req.Path("id"),
		Page:    req.Query("page"),
		Flag:    req.Query("flag"),
		TraceID: req.Header("X-Trace-ID"),
		Name:    req.Body.Name,
	}, nil
}

func decodeJSONBody[T any](t *testing.T, body []byte) T {
	t.Helper()
	var data T
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("unmarshal data failed: %v; body = %s", err, string(body))
	}
	return data
}

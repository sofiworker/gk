package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type terminalNoInputResp struct {
	OK bool `json:"ok"`
}

type terminalNoOutputReq struct {
	ID string `path:"id"`
}

type terminalNoOutputQueryReq struct {
	Name string `query:"name"`
}

type terminalGroupReq struct {
	ID string `path:"id"`
}

func TestRouteBuilderToNoInputIgnoresRequestInput(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, terminalNoInputResp](app).GET("/ping").ToNoInput(func(ctx context.Context) (terminalNoInputResp, error) {
		return terminalNoInputResp{OK: true}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", strings.NewReader(`{"ignored":true}`))
	req.Header.Set("Content-Type", MIMEJSON)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != MIMEJSON {
		t.Fatalf("content type = %q, want %q", ct, MIMEJSON)
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("body = %q, want ok=true", w.Body.String())
	}
}

func TestRouteBuilderToNoInputErrorUsesUnifiedPipeline(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, terminalNoInputResp](app).GET("/fail").ToNoInput(func(ctx context.Context) (terminalNoInputResp, error) {
		return terminalNoInputResp{}, Err(http.StatusBadRequest, "no-input boom")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fail", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "no-input boom") {
		t.Fatalf("body = %q, want no-input boom", w.Body.String())
	}
}

func TestRouteBuilderToNoOutputDefaultsToNoContent(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[terminalNoOutputReq, struct{}](app).DELETE("/users/{id}").ToNoOutput(func(ctx context.Context, req terminalNoOutputReq) error {
		if req.ID != "u1" {
			return Err(http.StatusBadRequest, "unexpected id "+req.ID)
		}
		return nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/u1", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", w.Body.String())
	}
}

func TestRouteBuilderToNoOutputStatusOverride(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[terminalNoOutputReq, struct{}](app).
		DELETE("/users/{id}").
		Status(http.StatusAccepted).
		ToNoOutput(func(ctx context.Context, req terminalNoOutputReq) error {
			return nil
		})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/u1", nil))

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", w.Body.String())
	}
}

func TestRouteBuilderToNoOutputParsesAndValidatesInput(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[terminalNoOutputReq, struct{}](app).
		DELETE("/users/{id}").
		Validate(func(ctx context.Context, req terminalNoOutputReq) error {
			if req.ID == "" {
				return Err(http.StatusUnprocessableEntity, "id required")
			}
			return nil
		}).
		ToNoOutput(func(ctx context.Context, req terminalNoOutputReq) error {
			return Err(http.StatusBadRequest, "boom")
		})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/u1", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("body = %q, want boom", w.Body.String())
	}
}

func TestRouteBuilderToNoOutputValidationRejects(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[terminalNoOutputQueryReq, struct{}](app).
		GET("/check").
		Validate(func(ctx context.Context, req terminalNoOutputQueryReq) error {
			if req.Name == "" {
				return Err(http.StatusUnprocessableEntity, "name required")
			}
			return nil
		}).
		ToNoOutput(func(ctx context.Context, req terminalNoOutputQueryReq) error {
			return nil
		})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/check", nil))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
}

func TestRouteBuilderGroupBranchLikeGin(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var mwRan bool
	api := Route[struct{}, struct{}](app).Group("/api", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwRan = true
			next.ServeHTTP(w, r)
		})
	})
	Route[terminalGroupReq, terminalNoInputResp](api).GET("/users/{id}").To(func(ctx context.Context, req terminalGroupReq) (terminalNoInputResp, error) {
		return terminalNoInputResp{OK: req.ID == "u1"}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/u1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !mwRan {
		t.Fatal("group middleware did not run")
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("body = %q, want ok=true", w.Body.String())
	}
}

func TestRouteBuilderGroupBranchNested(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	api := Route[struct{}, struct{}](app).Group("/api")
	v1 := Route[struct{}, struct{}](api).Group("/v1")
	Route[terminalGroupReq, terminalNoInputResp](v1).GET("/users/{id}").To(func(ctx context.Context, req terminalGroupReq) (terminalNoInputResp, error) {
		return terminalNoInputResp{OK: req.ID == "u1"}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/users/u1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestRouteBuilderGroupAfterMethodSetPanics(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic")
		}
		err, ok := recovered.(error)
		if !ok || !errors.Is(err, ErrRouteMethodAlreadySet) {
			t.Fatalf("panic = %v, want ErrRouteMethodAlreadySet", recovered)
		}
	}()
	Route[struct{}, struct{}](app).GET("/x").Group("/api")
}

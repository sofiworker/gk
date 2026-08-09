package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testOptionInput/Output 复用 builder_test.go 的形态，但字段独立以免依赖。
type testOptionInput struct {
	Params `json:"-"`

	Body struct {
		Name string `json:"name"`
	}
}

type testOptionOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

func optionHandler(ctx context.Context, req testOptionInput) (testOptionOutput, error) {
	return testOptionOutput{
		Body: struct {
			ID string `json:"id"`
		}{ID: req.Path("id")},
	}, nil
}

func TestRouteOptionGroupOptions(t *testing.T) {
	app := New(WithOpenAPI("opt", "1.0.0"), WithProduces(MIMEJSON))

	userOps := GroupOptions(
		OptDoc(Summary("获取用户"), Tags("users")),
		OptStatus(http.StatusAccepted),
	)

	Route[testOptionInput, testOptionOutput](app).
		GET("/users/{id}").
		Apply(userOps).
		To(optionHandler)

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/u1", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}

	spec := serverOpenAPISpec(t, app)
	s := string(spec)
	if !strings.Contains(s, `"获取用户"`) {
		t.Fatalf("OpenAPI spec missing summary, got %s", s)
	}
	if !strings.Contains(s, `"users"`) {
		t.Fatalf("OpenAPI spec missing tags, got %s", s)
	}
}

func TestRouteOptionProducesAndMiddleware(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[testOptionInput, testOptionOutput](app).
		POST("/users/{id}").
		Apply(
			OptUse(middlewareSetHeader("X-From-Option", "yes")),
			OptProduces(MIMEJSON),
		).
		To(optionHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/users/u1", strings.NewReader(`{"name":"Alice"}`))
	r.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(w, r)

	if got := w.Header().Get("X-From-Option"); got != "yes" {
		t.Fatalf("middleware header = %q, want %q", got, "yes")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, MIMEJSON) {
		t.Fatalf("content type = %q, want json", ct)
	}
}

func TestRouteOptionNilSkipped(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[testOptionInput, testOptionOutput](app).
		GET("/users/{id}").
		Apply(nil, OptStatus(http.StatusNoContent), nil).
		To(optionHandler)

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/u1", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestRouteOptionGroupOptionsSkipsNil(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	ops := GroupOptions(OptStatus(http.StatusCreated), nil)

	Route[testOptionInput, testOptionOutput](app).
		POST("/users/{id}").
		Apply(ops).
		To(optionHandler)

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users/u1", strings.NewReader(`{"name":"Alice"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestRouteOptionAfterFinalizePanics(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	b := Route[testOptionInput, testOptionOutput](app).
		GET("/users/{id}")

	b.To(optionHandler)

	assertPanicsWith(t, ErrRouteBuilderFinalized, func() {
		b.Apply(OptStatus(http.StatusOK))
	})
}

func TestRouteOptionGroupAfterFinalizePanics(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	b := Route[testOptionInput, testOptionOutput](app).
		GET("/users/{id}")

	b.To(optionHandler)

	assertPanicsWith(t, ErrRouteBuilderFinalized, func() {
		b.Apply(GroupOptions(OptProduces(MIMEJSON)))
	})
}

func middlewareSetHeader(name, value string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(name, value)
			next.ServeHTTP(w, r)
		})
	}
}

func assertPanicsWith(t *testing.T, want error, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic with %v, got none", want)
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("panic value = %#v, want error %v", r, want)
		}
		if !strings.Contains(err.Error(), want.Error()) {
			t.Fatalf("panic error = %v, want containing %v", err, want)
		}
	}()
	fn()
}

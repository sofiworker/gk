//go:build go1.27

package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type go127Params struct {
	ID string `path:"id"`
}

type go127Resp struct {
	ID string `json:"id"`
}

func TestGo127RouteChainInference(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.GET("/users/{id}").To(func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/u1", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"id":"u1"`) {
		t.Fatalf("body = %q, want id u1", w.Body.String())
	}
}

func TestGo127RouteChainStatusAndOptions(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.POST("/users").
		Status(http.StatusCreated).
		To(func(ctx context.Context, in *go127Params) (*go127Resp, error) {
			return &go127Resp{ID: in.ID}, nil
		})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users", nil))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
}

func TestGo127RouteChainNoInputNoOutput(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.GET("/health").ToNoInput(func(ctx context.Context) (*go127Resp, error) {
		return &go127Resp{ID: "ok"}, nil
	})
	app.DELETE("/users/{id}").ToNoOutput(func(ctx context.Context, in *go127Params) error {
		if in.ID != "u1" {
			return Err(http.StatusBadRequest, "bad id")
		}
		return nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"ok"`) {
		t.Fatalf("health status = %d body = %q", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/u1", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("delete body = %q, want empty", w.Body.String())
	}
}

func TestGo127ServerShorthands(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Get("/users/{id}", func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})
	app.Post("/users", func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})

	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/users/u1", http.StatusOK},
		{http.MethodPost, "/users", http.StatusOK},
	} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
}

func TestGo127GroupRoute(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	group := app.Group("/api")
	group.GET("/users/{id}").To(func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})
	group.Get("/pings", func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/u1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"u1"`) {
		t.Fatalf("group route status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestGo127BuilderGroupBranchLikeGin(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var mwRan bool
	api := app.Group("/api", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwRan = true
			next.ServeHTTP(w, r)
		})
	})
	api.GET("/users/{id}").To(func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})
	api.Get("/pings", func(ctx context.Context, _ struct{}) (*go127Resp, error) {
		return &go127Resp{ID: "pong"}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/u1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"u1"`) {
		t.Fatalf("builder group route status = %d body = %q", w.Code, w.Body.String())
	}
	if !mwRan {
		t.Fatal("builder group middleware did not run")
	}

	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/pings", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"pong"`) {
		t.Fatalf("builder group shorthand status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestGo127LegacyRouteSpellingStillWorks(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[go127Params, go127Resp](app).GET("/users/{id}").To(func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/u1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"u1"`) {
		t.Fatalf("legacy spelling status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestGo127ServerDirectVerbChain(t *testing.T) {
	app := New(WithOpenAPI("go127", "1.0.0"), WithProduces(MIMEJSON))
	app.GET("/users/{id}").
		Doc(Summary("get user"), OperationID("getUser")).
		To(func(ctx context.Context, in *go127Params) (*go127Resp, error) {
			return &go127Resp{ID: in.ID}, nil
		})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/u1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"u1"`) {
		t.Fatalf("direct GET chain status = %d body = %q", w.Code, w.Body.String())
	}

	doc, err := app.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), `"operationId":"getUser"`) {
		t.Fatalf("openapi missing operationId getUser: %s", string(doc))
	}
}

func TestGo127GroupDirectVerbChain(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	api := app.Group("/api")
	api.GET("/users/{id}").To(func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})
	api.POST("/users").Status(http.StatusCreated).To(func(ctx context.Context, in *go127Params) (*go127Resp, error) {
		return &go127Resp{ID: in.ID}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/u1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"u1"`) {
		t.Fatalf("group GET chain status = %d body = %q", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/users", nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("group POST chain status = %d, want 201", w.Code)
	}
}

func TestGo127ServerAnyChain(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.ANY("/ping").ToNoInput(func(ctx context.Context) (*go127Resp, error) {
		return &go127Resp{ID: "pong"}, nil
	})

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(method, "/ping", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"pong"`) {
			t.Fatalf("%s /ping status = %d body = %q", method, w.Code, w.Body.String())
		}
	}
}

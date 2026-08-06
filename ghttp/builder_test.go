package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type testInput struct {
	Params `json:"-"`

	Body struct {
		Name string `json:"name"`
	}
}

type testOutput struct {
	Status int `default:"200"`
	Body   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
}

func testHandler(ctx context.Context, req testInput) (testOutput, error) {
	return testOutput{
		Body: struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{
			ID:   req.Path("id"),
			Name: req.Body.Name,
		},
	}, nil
}

func TestRouteBuilderWithPOST(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[testInput, testOutput](app).
		POST("/users/{id}").
		Doc(
			Summary("Create user"),
			Tags("Users"),
			OperationID("createUser"),
		).
		To(testHandler)

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"Alice"}`)
	r := httptest.NewRequest("POST", "/users/42", body)
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var data struct {
		Status int `json:"Status"`
		Body   struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"Body"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if data.Body.Name != "Alice" {
		t.Fatalf("expected Name=Alice, got %s", data.Body.Name)
	}
	if data.Body.ID != "42" {
		t.Fatalf("expected ID=42, got %s", data.Body.ID)
	}
}

type cookieOutput struct {
	Token string `json:"token"`
}

func (o cookieOutput) Cookies() []*http.Cookie {
	return []*http.Cookie{
		{
			Name:     "session_id",
			Value:    o.Token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		},
		DeleteCookie("old_session"),
	}
}

func TestRouteBuilderWritesOutputCookies(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[Params, cookieOutput](app).GET("/login").To(func(ctx context.Context, params Params) (cookieOutput, error) {
		return cookieOutput{Token: params.DefaultCookie("seed", "token-1")}, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: "seed", Value: "token-2"})
	app.ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies = %#v, want session and deletion cookies", cookies)
	}
	if cookies[0].Name != "session_id" || cookies[0].Value != "token-2" || !cookies[0].HttpOnly {
		t.Fatalf("session cookie = %#v", cookies[0])
	}
	if cookies[1].Name != "old_session" || cookies[1].MaxAge != -1 {
		t.Fatalf("delete cookie = %#v, want MaxAge -1", cookies[1])
	}
}

func TestRouteBuilderConsumesRejectsUnsupportedContentType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	Route[input, struct{}](app).
		POST("/users").
		Consumes(MIMEJSON).
		To(func(context.Context, input) (struct{}, error) {
			t.Fatal("handler should not run for unsupported media type")
			return struct{}{}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Content-Type", MIMEPlain)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnsupportedMediaType, rec.Body.String())
	}
}

func TestRouteBuilderErrorWithoutEnvelopeUsesJSON(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[struct{}, struct{}](app).GET("/posts/{id}").To(func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, NotFound("post not found")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/posts/999", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != MIMEJSON {
		t.Fatalf("Content-Type = %q, want %s; body = %s", got, MIMEJSON, rec.Body.String())
	}
	var body HTTPError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body failed: %v; body = %s", err, rec.Body.String())
	}
	if body.Code != http.StatusNotFound || body.Message != "post not found" {
		t.Fatalf("error body = %#v, want 404 post not found", body)
	}
}

func TestRouteBuilderConsumesAllowsEmptyContentType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}
	type output struct {
		Name string `json:"name"`
	}

	Route[input, output](app).
		POST("/users").
		Consumes(MIMEJSON).
		To(func(ctx context.Context, req input) (output, error) {
			return output{Name: req.Body.Name}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"alice"}`))
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v; body = %s", err, rec.Body.String())
	}
	if data.Name != "alice" {
		t.Fatalf("Name = %q, want alice", data.Name)
	}
}

func TestRouteBuilderConsumesMatchesContentTypeParameters(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	Route[input, struct{}](app).
		POST("/users").
		Consumes(MIMEJSON).
		To(func(ctx context.Context, req input) (struct{}, error) {
			if req.Body.Name != "alice" {
				t.Fatalf("Name = %q, want alice", req.Body.Name)
			}
			return struct{}{}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Content-Type", MIMEJSON+"; charset=utf-8")
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRouteBuilderConsumesInheritanceAndOverride(t *testing.T) {
	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	app := New(WithProduces(MIMEJSON)).Consumes(MIMEJSON)
	api := app.Group("/api").Consumes(MIMEXML)

	Route[input, struct{}](api).POST("/xml").To(func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	})
	Route[input, struct{}](api).POST("/json").Consumes(MIMEJSON).To(func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	})

	tests := []struct {
		name        string
		path        string
		contentType string
		body        string
		want        int
	}{
		{name: "group consumes xml", path: "/api/xml", contentType: MIMEXML, body: `<Body><Name>alice</Name></Body>`, want: http.StatusOK},
		{name: "group rejects server json default", path: "/api/xml", contentType: MIMEJSON, body: `{"name":"alice"}`, want: http.StatusUnsupportedMediaType},
		{name: "route override consumes json", path: "/api/json", contentType: MIMEJSON, body: `{"name":"alice"}`, want: http.StatusOK},
		{name: "route override rejects xml", path: "/api/json", contentType: MIMEXML, body: `<Body><Name>alice</Name></Body>`, want: http.StatusUnsupportedMediaType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			app.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestServerWithBodyDecoderUsesInstanceDecoder(t *testing.T) {
	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}
	type output struct {
		Name string `json:"name"`
	}

	newApp := func(prefix string) *Server {
		app := New(WithProduces(MIMEJSON), WithBodyDecoder(func(r io.Reader, contentType string, target interface{}) error {
			data, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			body := reflect.ValueOf(target).Elem()
			body.FieldByName("Name").SetString(prefix + string(data))
			return nil
		}))
		Route[input, output](app).POST("/decode").To(func(ctx context.Context, req input) (output, error) {
			return output{Name: req.Body.Name}, nil
		})
		return app
	}

	for _, tt := range []struct {
		name string
		app  *Server
		want string
	}{
		{name: "first server", app: newApp("one:"), want: "one:alice"},
		{name: "second server", app: newApp("two:"), want: "two:alice"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/decode", strings.NewReader("alice"))
			req.Header.Set("Content-Type", "application/x-custom")
			tt.app.ServeHTTP(rec, req)

			var data output
			if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
				t.Fatalf("unmarshal response failed: %v; body = %s", err, rec.Body.String())
			}
			if data.Name != tt.want {
				t.Fatalf("Name = %q, want %q", data.Name, tt.want)
			}
		})
	}
}

func TestRouteBuilderRejectsBodyOverServerMaxBodyBytes(t *testing.T) {
	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	app := New(WithProduces(MIMEJSON), WithMaxBodyBytes(16))

	Route[input, struct{}](app).
		POST("/users").
		To(func(context.Context, input) (struct{}, error) {
			t.Fatal("handler should not run for oversized request body")
			return struct{}{}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"alice-over-limit"}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestRouteBuilderMaxBodyBytesEnvelopeUsesPayloadTooLargeCode(t *testing.T) {
	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	app := New(WithProduces(MIMEJSON), WithEnvelope(DefaultEnvelope), WithMaxBodyBytes(16))

	Route[input, struct{}](app).
		POST("/users").
		To(func(context.Context, input) (struct{}, error) {
			t.Fatal("handler should not run for oversized request body")
			return struct{}{}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"alice-over-limit"}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	var env struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope failed: %v; body = %s", err, rec.Body.String())
	}
	if env.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("envelope code = %d, want %d; body = %s", env.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestRouteBuilderMaxBodyBytesOverridesServerLimit(t *testing.T) {
	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}
	type output struct {
		Name string `json:"name"`
	}

	app := New(WithProduces(MIMEJSON), WithMaxBodyBytes(8))

	Route[input, output](app).
		POST("/users").
		MaxBodyBytes(64).
		To(func(ctx context.Context, req input) (output, error) {
			return output{Name: req.Body.Name}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v; body = %s", err, rec.Body.String())
	}
	if data.Name != "alice" {
		t.Fatalf("Name = %q, want alice", data.Name)
	}
}

func TestNewUsesDefaultMaxBodyBytes(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	if app.config.maxBodyBytes != DefaultMaxBodyBytes {
		t.Fatalf("maxBodyBytes = %d, want %d", app.config.maxBodyBytes, DefaultMaxBodyBytes)
	}
}

func TestRouteBuilderPostShortcutReplacement(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[testInput, testOutput](app).POST("/users/{id}").To(testHandler)

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"Bob"}`)
	r := httptest.NewRequest("POST", "/users/99", body)
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var data testOutput
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if data.Body.Name != "Bob" || data.Body.ID != "99" {
		t.Fatalf("data = %#v", data)
	}
}

func TestRouteBuilderPointerResponseType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type output struct {
		Message string `json:"message"`
	}

	Route[struct{}, *output](app).GET("/pointer-response").To(func(ctx context.Context, req struct{}) (*output, error) {
		return &output{Message: "ok"}, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pointer-response", nil)
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if data.Message != "ok" {
		t.Fatalf("message = %q, want ok", data.Message)
	}
}

func TestRouteBuilderPointerRequestType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Params `json:"-"`
	}
	type output struct {
		Name string `json:"name"`
	}

	Route[*input, output](app).GET("/pointer-request").To(func(ctx context.Context, req *input) (output, error) {
		if req == nil {
			t.Fatal("request input is nil")
		}
		return output{Name: req.Query("name")}, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pointer-request?name=alice", nil)
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if data.Name != "alice" {
		t.Fatalf("name = %q, want alice", data.Name)
	}
}

func TestRouteBuilderToRequiresProduces(t *testing.T) {
	app := New()

	assertPanicsIs(t, ErrRouteProducesRequired, func() {
		Route[struct{}, string](app).GET("/ping").To(func(ctx context.Context, req struct{}) (string, error) {
			return "pong", nil
		})
	})
}

func TestRouteBuilderProducesPlainString(t *testing.T) {
	app := New()

	Route[struct{}, string](app).
		GET("/ping").
		Produces(MIMEPlain).
		To(func(ctx context.Context, req struct{}) (string, error) {
			return "pong", nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "*/*")
	app.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != MIMEPlain {
		t.Fatalf("Content-Type = %q, want %s", ct, MIMEPlain)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "pong" {
		t.Fatalf("body = %q, want pong", got)
	}
}

func TestRouteBuilderProducesInheritance(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	group := app.Group("/api").Produces(MIMEPlain)

	Route[struct{}, string](app).GET("/server").To(func(context.Context, struct{}) (string, error) {
		return "server", nil
	})

	Route[struct{}, string](group).GET("/group").To(func(context.Context, struct{}) (string, error) {
		return "group", nil
	})

	Route[struct{}, string](group).GET("/route").Produces(MIMEJSON).To(func(context.Context, struct{}) (string, error) {
		return "route", nil
	})

	tests := []struct {
		path string
		ct   string
		body string
	}{
		{path: "/server", ct: MIMEJSON, body: `"server"`},
		{path: "/api/group", ct: MIMEPlain, body: "group"},
		{path: "/api/route", ct: MIMEJSON, body: `"route"`},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		req.Header.Set("Accept", "*/*")
		app.ServeHTTP(rec, req)

		if ct := rec.Header().Get("Content-Type"); ct != tt.ct {
			t.Fatalf("%s Content-Type = %q, want %s", tt.path, ct, tt.ct)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != tt.body {
			t.Fatalf("%s body = %q, want %q", tt.path, got, tt.body)
		}
	}
}

func TestRouteBuilderToRaw(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[struct{}, struct{}](app).GET("/raw").ToRaw(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/raw", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRouteBuilderToSSE(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[struct{}, struct{}](app).GET("/events").ToSSE(func(ctx context.Context, params Params, stream *SSEWriter) error {
		return stream.WriteEvent("message", "hello")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	app.ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(rec.Body.String(), "data: hello") {
		t.Fatalf("body = %q, want SSE payload", rec.Body.String())
	}
}

func TestRouteBuilderToHTML(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(`<h1>{{.Title}}</h1>`), 0o600); err != nil {
		t.Fatalf("write template failed: %v", err)
	}

	app := New(WithRenderer(NewRenderer(tmpDir, ".html", template.FuncMap{}, false)))
	Route[struct{}, struct{}](app).GET("/page").ToHTML(http.StatusCreated, "index", map[string]interface{}{"Title": "Hello"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
}

func TestRouteBuilderPathAndQueryPopulateOpenAPI(t *testing.T) {
	app := New(WithOpenAPI("api", "1.0.0"), WithProduces(MIMEJSON))

	type input struct {
		ID   string `path:"id"`
		Role string `query:"role"`
	}

	Route[input, struct{}](app).
		GET("/users/{id}").
		To(func(context.Context, input) (struct{}, error) {
			return struct{}{}, nil
		})

	spec := serverOpenAPISpec(t, app)
	var doc map[string]interface{}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("unmarshal openapi failed: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	item := paths["/users/{id}"].(map[string]interface{})
	op := item["get"].(map[string]interface{})
	params := op["parameters"].([]interface{})
	if len(params) != 2 {
		t.Fatalf("parameters = %#v, want 2 entries", params)
	}
}

func TestRouteBuilderParamsSnapshot(t *testing.T) {
	app := New(WithClientIPResolver(func(r *http.Request) string {
		return r.Header.Get("X-Client-IP")
	}), WithProduces(MIMEJSON))

	type input struct {
		Params `json:"-"`
	}
	type output struct {
		Name     string   `json:"name"`
		Role     string   `json:"role"`
		Tags     []string `json:"tags"`
		Token    string   `json:"token"`
		ClientIP string   `json:"client_ip"`
	}

	Route[input, output](app).
		PUT("/user/{name}").
		To(func(ctx context.Context, in input) (output, error) {
			return output{
				Name:     in.Path("name"),
				Role:     in.DefaultQuery("role", "guest"),
				Tags:     in.QueryList("tag"),
				Token:    in.Header("X-Token"),
				ClientIP: in.ClientIP(),
			}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/user/alice?age=18&tag=a&tag=b", nil)
	req.Header.Set("X-Token", "token-1")
	req.Header.Set("X-Client-IP", "203.0.113.9")
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if data.Name != "alice" || data.Role != "guest" || data.Token != "token-1" || data.ClientIP != "203.0.113.9" {
		t.Fatalf("data = %#v", data)
	}
	if !reflect.DeepEqual(data.Tags, []string{"a", "b"}) {
		t.Fatalf("tags = %#v", data.Tags)
	}
}

func TestRouteBuilderHandlerUsesInputParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Params `json:"-"`
	}
	type output struct {
		ID      string `json:"id"`
		Role    string `json:"role"`
		Missing string `json:"missing"`
	}

	Route[input, output](app).GET("/users/{id}").To(func(ctx context.Context, req input) (output, error) {
		return output{
			ID:      req.Path("id"),
			Role:    req.DefaultQuery("role", "guest"),
			Missing: req.DefaultPath("missing", "fallback"),
		}, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42?role=admin", nil)
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if data.ID != "42" || data.Role != "admin" || data.Missing != "fallback" {
		t.Fatalf("data = %#v", data)
	}
}

func TestRouteBuilderSupportsDirectParamsInput(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type output struct {
		ID    string `json:"id"`
		Role  string `json:"role"`
		Token string `json:"token"`
	}

	Route[Params, output](app).GET("/direct/{id}").To(func(ctx context.Context, params Params) (output, error) {
		return output{
			ID:    params.Path("id"),
			Role:  params.DefaultQuery("role", "guest"),
			Token: params.Header("X-Token"),
		}, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/direct/42?role=admin", nil)
	req.Header.Set("X-Token", "secret")
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v; body = %s", err, rec.Body.String())
	}
	if data.ID != "42" || data.Role != "admin" || data.Token != "secret" {
		t.Fatalf("data = %#v", data)
	}
}

func TestRouteBuilderDirectParamsPassesPointerToCustomValidator(t *testing.T) {
	var validated any
	app := New(
		WithProduces(MIMEJSON),
		WithValidator(serverValidatorFunc(func(_ context.Context, input interface{}) error {
			validated = input
			return nil
		})),
	)

	Route[Params, struct{}](app).GET("/direct-validator/{id}").ToHTTPFunc(func(_ http.ResponseWriter, _ *http.Request, params Params) error {
		if got, want := params.Path("id"), "42"; got != want {
			t.Fatalf("handler path id = %q, want %q", got, want)
		}
		return nil
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/direct-validator/42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	params, ok := validated.(*Params)
	if !ok {
		t.Fatalf("validator input type = %T, want *Params", validated)
	}
	if got, want := params.Path("id"), "42"; got != want {
		t.Fatalf("validator path id = %q, want %q", got, want)
	}
}

func TestRouteBuilderRejectsDirectPointerParamsInput(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	assertPanicsIs(t, ErrInvalidParamsUsage, func() {
		Route[*Params, struct{}](app).GET("/direct-pointer-params").To(func(context.Context, *Params) (struct{}, error) {
			return struct{}{}, nil
		})
	})
}

func TestRouteBuilderSetupErrorIncludesRouteAndCaller(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	var got error
	func() {
		defer func() {
			recovered := recover()
			if recovered == nil {
				t.Fatal("expected setup error panic")
			}
			err, ok := recovered.(error)
			if !ok {
				t.Fatalf("panic = %v, want error", recovered)
			}
			got = err
		}()
		Route[struct{}, struct{}](app).
			GET("/broken").
			Produces("application/unsupported").
			To(func(context.Context, struct{}) (struct{}, error) {
				return struct{}{}, nil
			})
	}()

	if !errors.Is(got, ErrRouteProducesUnsupported) {
		t.Fatalf("panic error = %v, want %v", got, ErrRouteProducesUnsupported)
	}
	msg := got.Error()
	for _, want := range []string{"GET /broken"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("panic error = %q, want to contain %q", msg, want)
		}
	}
}

func TestRouteBuilderRejectsInvalidParamsUsage(t *testing.T) {
	type namedParamsInput struct {
		Request Params `json:"-"`
	}
	type pointerParamsInput struct {
		*Params `json:"-"`
	}
	type commonParams struct {
		Params `json:"-"`
	}
	type indirectParamsInput struct {
		commonParams
	}

	tests := []struct {
		name     string
		register func(*Server)
	}{
		{
			name: "named params",
			register: func(app *Server) {
				Route[namedParamsInput, struct{}](app).GET("/named-params").To(func(context.Context, namedParamsInput) (struct{}, error) {
					return struct{}{}, nil
				})
			},
		},
		{
			name: "pointer params",
			register: func(app *Server) {
				Route[pointerParamsInput, struct{}](app).GET("/pointer-params").To(func(context.Context, pointerParamsInput) (struct{}, error) {
					return struct{}{}, nil
				})
			},
		},
		{
			name: "indirect params",
			register: func(app *Server) {
				Route[indirectParamsInput, struct{}](app).GET("/indirect-params").To(func(context.Context, indirectParamsInput) (struct{}, error) {
					return struct{}{}, nil
				})
			},
		},
		{
			name: "http func params",
			register: func(app *Server) {
				Route[namedParamsInput, struct{}](app).GET("/http-func-params").ToHTTPFunc(func(http.ResponseWriter, *http.Request, namedParamsInput) error {
					return nil
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPanicsIs(t, ErrInvalidParamsUsage, func() {
				tt.register(New(WithProduces(MIMEJSON)))
			})
		})
	}
}

func TestRouteBuilderToHTTPFunc(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Params `json:"-"`
	}

	Route[input, struct{}](app).GET("/raw-context").ToHTTPFunc(func(w http.ResponseWriter, r *http.Request, req input) error {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodGet)
		}
		value := req.Query("value")
		if value != "yes" {
			t.Fatalf("value = %q, want yes", value)
		}
		w.Header().Set("X-Raw-Context", value)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("handled"))
		return nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/raw-context?value=yes", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if got := rec.Header().Get("X-Raw-Context"); got != "yes" {
		t.Fatalf("X-Raw-Context = %q, want yes", got)
	}
	if rec.Body.String() != "handled" {
		t.Fatalf("body = %q, want handled", rec.Body.String())
	}
}

func TestRouteBuilderToRedirect(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[struct{}, struct{}](app).GET("/old").ToRedirect(http.StatusFound, "/new")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/old", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/new" {
		t.Fatalf("Location = %q, want /new", got)
	}
}

func TestRouteBuilderToRedirectFunc(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Params `json:"-"`
	}

	Route[input, struct{}](app).GET("/old/{id}").ToRedirectFunc(http.StatusMovedPermanently, func(req input) (string, error) {
		return "/new/" + req.Path("id"), nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/old/42", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if got := rec.Header().Get("Location"); got != "/new/42" {
		t.Fatalf("Location = %q, want /new/42", got)
	}
}

func TestSSEHandlerUsesParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	Route[struct{}, struct{}](app).GET("/events/{id}").ToSSE(func(ctx context.Context, params Params, stream *SSEWriter) error {
		if err := stream.WriteEvent("path", params.Path("id")); err != nil {
			return err
		}
		return stream.WriteEvent("query", params.DefaultQuery("role", "guest"))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/7?role=admin", nil)
	app.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "data: 7") || !strings.Contains(body, "data: admin") {
		t.Fatalf("body = %q", body)
	}
}

func TestRouteBuilderProducesNegotiatesConfiguredContentTypes(t *testing.T) {
	app := New(WithProduces(MIMEJSON, MIMEXML), WithLenientContentNegotiation())

	type output struct {
		Message string `json:"message" xml:"message"`
	}

	Route[struct{}, output](app).
		GET("/negotiated").
		To(func(context.Context, struct{}) (output, error) {
			return output{Message: "hello"}, nil
		})

	xmlRec := httptest.NewRecorder()
	xmlReq := httptest.NewRequest(http.MethodGet, "/negotiated", nil)
	xmlReq.Header.Set("Accept", MIMEXML)
	app.ServeHTTP(xmlRec, xmlReq)

	if ct := xmlRec.Header().Get("Content-Type"); ct != MIMEXML {
		t.Fatalf("xml Content-Type = %q, want %s", ct, MIMEXML)
	}
	if body := xmlRec.Body.String(); !strings.Contains(body, "<message>hello</message>") {
		t.Fatalf("xml body = %q, want message element", body)
	}

	fallbackRec := httptest.NewRecorder()
	fallbackReq := httptest.NewRequest(http.MethodGet, "/negotiated", nil)
	fallbackReq.Header.Set("Accept", MIMEPlain)
	app.ServeHTTP(fallbackRec, fallbackReq)

	if ct := fallbackRec.Header().Get("Content-Type"); ct != MIMEJSON {
		t.Fatalf("fallback Content-Type = %q, want %s", ct, MIMEJSON)
	}
	if body := strings.TrimSpace(fallbackRec.Body.String()); body != `{"message":"hello"}` {
		t.Fatalf("fallback body = %q, want JSON", body)
	}
}

func TestRouteBuilderRouteProducesOverridesServerProducesList(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type output struct {
		Message string `json:"message" xml:"message"`
	}

	Route[struct{}, output](app).
		GET("/route-produces").
		Produces(MIMEXML, MIMEJSON).
		To(func(context.Context, struct{}) (output, error) {
			return output{Message: "route"}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/route-produces", nil)
	req.Header.Set("Accept", MIMEJSON)
	app.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != MIMEJSON {
		t.Fatalf("Content-Type = %q, want %s", ct, MIMEJSON)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"message":"route"}` {
		t.Fatalf("body = %q, want JSON", body)
	}
}

func TestRouteBuilderValidateStopsHandlerWithDefaultValidationError(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	called := false

	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	Route[input, struct{}](app).
		POST("/validated").
		Validate(
			func(req input) error {
				if req.Body.Name == "" {
					return errors.New("name required")
				}
				return nil
			},
			ValidationError(Err(http.StatusUnprocessableEntity, "name required")),
		).
		To(func(context.Context, input) (struct{}, error) {
			called = true
			return struct{}{}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/validated", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if called {
		t.Fatal("handler called after validation failure")
	}
	if !strings.Contains(rec.Body.String(), "name required") {
		t.Fatalf("body = %q, want validation message", rec.Body.String())
	}
}

func TestRouteBuilderValidateCanOverrideValidationError(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	Route[input, struct{}](app).
		POST("/mapped-validation").
		Validate(
			func(context.Context, input) error {
				return errors.New("raw validation failure")
			},
			ValidationError(BadRequest("invalid input")),
		).
		To(func(context.Context, input) (struct{}, error) {
			return struct{}{}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mapped-validation", strings.NewReader(`{"name":""}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid input") {
		t.Fatalf("body = %q, want mapped validation message", rec.Body.String())
	}
}

func TestRouteBuilderSkipValidationSkipsGlobalValidatorOnly(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithValidator(serverValidatorFunc(func(context.Context, interface{}) error {
		return errors.New("global validation should be skipped")
	})))
	called := false

	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	Route[input, struct{}](app).
		POST("/skip-global-validation").
		SkipValidation().
		Validate(
			func(req input) error {
				if req.Body.Name == "" {
					return errors.New("route validation still runs")
				}
				return nil
			},
			ValidationError(Err(http.StatusUnprocessableEntity, "route validation still runs")),
		).
		To(func(context.Context, input) (struct{}, error) {
			called = true
			return struct{}{}, nil
		})

	failRec := httptest.NewRecorder()
	failReq := httptest.NewRequest(http.MethodPost, "/skip-global-validation", strings.NewReader(`{}`))
	failReq.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(failRec, failReq)
	if failRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("route validation status = %d, want %d", failRec.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(failRec.Body.String(), "route validation still runs") {
		t.Fatalf("body = %q, want route validation message", failRec.Body.String())
	}

	okRec := httptest.NewRecorder()
	okReq := httptest.NewRequest(http.MethodPost, "/skip-global-validation", strings.NewReader(`{"name":"Alice"}`))
	okReq.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("success status = %d, want %d; body=%s", okRec.Code, http.StatusOK, okRec.Body.String())
	}
	if !called {
		t.Fatal("handler was not called after route validation passed")
	}
}

func TestServerValidatorOffByDefault(t *testing.T) {
	type input struct {
		Params
		Body struct {
			Name string `json:"name" validate:"required"`
		}
	}
	app := New(WithProduces(MIMEJSON))
	Route[input, struct{}](app).POST("/users").To(func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (validator must be off by default)", w.Code)
	}
}

func TestServerValidatorExplicitOn(t *testing.T) {
	type validatedInput struct {
		Params
		Body struct {
			Name string `json:"name" validate:"required"`
		}
	}
	app := New(WithProduces(MIMEJSON), WithValidator(newDefaultValidator()))
	Route[validatedInput, struct{}](app).POST("/users").To(func(context.Context, validatedInput) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":""}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
}

type createdResp struct {
	ID string `json:"id"`
}

func (r createdResp) StatusCode() int { return http.StatusCreated }

func (r createdResp) WriteResponseHeaders(h http.Header) {
	h.Set("X-Resource-ID", r.ID)
}

type noContentResp struct{}

func (noContentResp) StatusCode() int { return http.StatusNoContent }

func TestTypedRouteExplicitStatusAndHeaders(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, createdResp](app).POST("/users").To(func(context.Context, struct{}) (createdResp, error) {
		return createdResp{ID: "u-1"}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users", nil))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if got := w.Header().Get("X-Resource-ID"); got != "u-1" {
		t.Fatalf("header = %q, want u-1", got)
	}
}

func TestTypedRouteNoBodyStatus(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, noContentResp](app).DELETE("/users/{id}").To(func(context.Context, struct{}) (noContentResp, error) {
		return noContentResp{}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/1", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", w.Body.String())
	}
}

func TestTypedRouteBuilderFixedStatus(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct {
		Created bool `json:"created"`
	}](app).
		POST("/resources").
		Status(http.StatusCreated).
		ResponseHeader("X-Resource", "r-1").
		To(func(context.Context, struct{}) (struct {
			Created bool `json:"created"`
		}, error) {
			return struct {
				Created bool `json:"created"`
			}{Created: true}, nil
		})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/resources", nil))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if got := w.Header().Get("X-Resource"); got != "r-1" {
		t.Fatalf("header = %q, want r-1", got)
	}
}

package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type apiData struct {
	Name string `json:"name" form:"name" xml:"name"`
}

func TestUnifiedCodecs(t *testing.T) {
	for _, tc := range []struct {
		name, media, body string
		codec             Input[*apiData]
	}{
		{"json", "application/json", `{"name":"alice"}`, JSONInput[*apiData]()},
		{"form", "application/x-www-form-urlencoded", "name=alice", FormInput[*apiData]()},
		{"xml", "application/xml", "<user><name>alice</name></user>", XMLInput[*apiData]()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer()
			g := s.Group("/users").With(WithInput(tc.codec))
			h := func(ctx context.Context, req RequestOf[*apiData]) (string, error) {
				page, ok := req.QueryFirst("page")
				if !ok || page != "invalid" {
					t.Fatal("source missing")
				}
				data, err := req.Data(ctx)
				if err != nil {
					return "", err
				}
				return req.Path("id") + data.Name, nil
			}
			if err := g.Register(Patch("/{id}", h)); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				r := httptest.NewRequest("PATCH", "/users/42?page=invalid", strings.NewReader(tc.body))
				r.Header.Set("Content-Type", tc.media)
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				if w.Code != 200 || w.Body.String() != "\"42alice\"\n" {
					t.Fatalf("%d %q", w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestDefaultNoDataAndExplicitBinding(t *testing.T) {
	type params struct {
		ID   int `path:"id"`
		Page int `query:"page"`
	}
	s := NewServer()
	if err := s.Register(
		Get("/none/{id}", func(_ context.Context, req RequestOf[NoData]) (string, error) {
			b, err := io.ReadAll(req.Body)
			return req.Path("id") + string(b), err
		}),
		Post("/json", func(ctx context.Context, req RequestOf[*apiData]) (string, error) {
			data, err := req.Data(ctx)
			if err != nil {
				return "", err
			}
			if data == nil {
				return "nil", nil
			}
			return data.Name, nil
		}),
		Get("/bind/{id}", func(ctx context.Context, req RequestOf[params]) (int, error) {
			data, _ := req.Data(ctx)
			return data.ID + data.Page, nil
		}, WithInput(BindInput[params]())),
	); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, payload, want string
		status                      int
	}{
		{"GET", "/none/42?page=invalid", "not JSON", "\"42not JSON\"\n", 200},
		{"POST", "/json", `{"name":"alice"}`, "\"alice\"\n", 200},
		{"POST", "/json", "null", "\"nil\"\n", 200},
		{"GET", "/bind/42?page=2", "", "44\n", 200},
		{"GET", "/bind/42?page=invalid", "", "", 400},
	} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.payload)))
		if w.Code != tc.status || tc.status == 200 && w.Body.String() != tc.want {
			t.Fatalf("%s: %d %q", tc.path, w.Code, w.Body.String())
		}
	}
	if _, err := compileInput(BindInput[int]()); err == nil {
		t.Fatal("scalar binding accepted")
	}
}

func TestMiddlewareAndValidatorsOrder(t *testing.T) {
	var order []string
	marker := errors.New("invalid data")
	mw := func(label string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				order = append(order, label)
				return next(ctx, req, resp)
			}
		}
	}
	s := NewServer().Use(mw("global"))
	g := s.Group("/api").Use(mw("group"))
	if err := g.Register(Post("/", func(context.Context, RequestOf[apiData]) (string, error) {
		order = append(order, "handler")
		return "ok", nil
	},
		WithMiddleware(mw("route")),
		WithDataValidator(func(_ context.Context, data apiData) error {
			order = append(order, "data")
			if data.Name == "bad" {
				return marker
			}
			return nil
		}),
		WithValidator(func(context.Context, RequestOf[apiData]) error { order = append(order, "request"); return nil }),
	)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body   string
		status int
		want   []string
	}{
		{`{"name":"ok"}`, 200, []string{"global", "group", "route", "data", "request", "handler"}},
		{`{"name":"bad"}`, 400, []string{"global", "group", "route", "data"}},
		{`invalid`, 400, []string{"global", "group", "route"}},
	} {
		order = nil
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/api", strings.NewReader(tc.body)))
		if w.Code != tc.status || !reflect.DeepEqual(order, tc.want) {
			t.Fatalf("%d %v", w.Code, order)
		}
	}
	h := func(context.Context, RequestOf[apiData]) (string, error) { return "", nil }
	if Post("/", h, WithInput(JSONInput[string]())).Err() == nil {
		t.Fatal("mismatch accepted")
	}
	if Post("/", h, WithInput(DecodeRequest(JSONInput[apiData]()))).Err() == nil {
		t.Fatal("wrapper codec accepted")
	}
	if Post("/", h, WithDataValidator[string](nil)).Err() == nil {
		t.Fatal("invalid validator accepted")
	}
	if Post("/", h, WithInput(Input[apiData]{})).Err() == nil {
		t.Fatal("empty codec accepted")
	}
}

func TestBodyErrorsAndDeferredDecode(t *testing.T) {
	s := NewServer()
	if err := s.Register(
		Post("/required", func(ctx context.Context, req RequestOf[*apiData]) (string, error) {
			// 强制调用 Data() 触发 RequireBody 验证
			data, err := req.Data(ctx)
			if err != nil {
				return "", err
			}
			_ = data
			return "ok", nil
		}, WithInput(RequireBody(StrictJSONInput[*apiData]())), WithBodyLimit(25)),
		Post("/deferred", func(ctx context.Context, req RequestOf[NoData]) (string, error) {
			if req.HeaderValue("X-Skip").String() == "yes" {
				return "skip", nil
			}
			data, err := ReadBody(ctx, req.RequestInput, JSONInput[apiData]())
			return data.Name, err
		}, WithBodyLimit(25)),
	); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, body, media, skip string
		status                  int
	}{
		{"/required", "", "application/json", "", 400},
		{"/required", "null", "application/json", "", 200},
		{"/required", `{"extra":1}`, "application/json", "", 400},
		{"/required", `{}`, "text/plain", "", 415},
		{"/required", `{"name":"very long long long long"}`, "application/json", "", 413},
		{"/deferred", "invalid", "application/json", "yes", 200},
		{"/deferred", "invalid", "application/json", "", 400},
	} {
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.media)
		r.Header.Set("X-Skip", tc.skip)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %q: %d", tc.path, tc.body, w.Code)
		}
	}
}

func TestMultipartAndStreamUnifiedRequest(t *testing.T) {
	var buffer bytes.Buffer
	w := multipart.NewWriter(&buffer)
	part, err := w.CreateFormFile("file", "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	type upload struct {
		File *multipart.FileHeader `form:"file"`
	}
	s := NewServer()
	if err := s.Register(
		Post("/upload/{id}", func(ctx context.Context, req RequestOf[upload]) (string, error) {
			body, err := req.Data(ctx)
			if err != nil {
				return "", err
			}
			file, err := body.File.Open()
			if err != nil {
				return "", err
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			return req.Path("id") + string(data), err
		}, WithInput(MultipartInput[upload](MultipartLimits{MaxBytes: 1024, MemoryBytes: 1}))),
		Post("/stream/{id}", func(ctx context.Context, req RequestOf[*multipart.Reader]) (string, error) {
			reader, err := req.Data(ctx)
			if err != nil {
				return "", err
			}
			part, err := reader.NextPart()
			if err != nil {
				return "", err
			}
			defer part.Close()
			data, err := io.ReadAll(part)
			return req.Path("id") + string(data), err
		}, WithInput(MultipartStreamInput())),
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/upload/42", "/stream/42"} {
		r := httptest.NewRequest("POST", path, bytes.NewReader(buffer.Bytes()))
		r.Header.Set("Content-Type", w.FormDataContentType())
		out := httptest.NewRecorder()
		s.ServeHTTP(out, r)
		if out.Code != 200 || out.Body.String() != "\"42content\"\n" {
			t.Fatalf("%d %q", out.Code, out.Body.String())
		}
		if r.MultipartForm != nil {
			for _, files := range r.MultipartForm.File {
				for _, file := range files {
					if _, err := file.Open(); err == nil {
						t.Fatal("spilled file survived cleanup")
					}
				}
			}
		}
	}
}

func TestOpenAPIDataAndBinding(t *testing.T) {
	type params struct {
		Page int `query:"page"`
	}
	s := NewServer()
	if err := s.Register(
		Post("/users/{id}", func(context.Context, RequestOf[*apiData]) (string, error) { return "", nil }, WithInput(RequireBody(JSONInput[*apiData]())), WithParameter[int](ParameterPath, "id", true)),
		Get("/search", func(context.Context, RequestOf[params]) (string, error) { return "", nil }, WithInput(BindInput[params]())),
		Get("/health", func(context.Context, RequestOf[NoData]) (string, error) { return "", nil }),
	); err != nil {
		t.Fatal(err)
	}
	data, err := s.OpenAPI("v3", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	body := paths["/users/{id}"].(map[string]any)["post"].(map[string]any)["requestBody"].(map[string]any)
	if body["required"] != true {
		t.Fatal(body)
	}
	search := paths["/search"].(map[string]any)["get"].(map[string]any)
	if search["requestBody"] != nil || len(search["parameters"].([]any)) != 1 {
		t.Fatal(search)
	}
	health := paths["/health"].(map[string]any)["get"].(map[string]any)
	if health["requestBody"] != nil || health["x-gk-request-schema-unavailable"] != nil {
		t.Fatal(health)
	}
}

func TestNoInputActionAndRaw(t *testing.T) {
	s := NewServer()
	if err := s.Register(
		FromFunc(http.MethodGet, "/health", func(context.Context) (string, error) { return "ok", nil }),
		FromAction(http.MethodDelete, "/{id}", func(_ context.Context, req RequestOf[NoData]) error {
			if req.Path("id") != "42" {
				t.Fatal("path missing")
			}
			return nil
		}),
		FromProcedure(http.MethodPost, "/logout", func(context.Context) error { return nil }),
		Raw(http.MethodGet, "/raw", func(_ context.Context, _ *Request, resp *Response) error {
			_, err := resp.Write([]byte("raw"))
			return err
		}),
	); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/health", 200}, {"DELETE", "/42", 204}, {"POST", "/logout", 204}, {"GET", "/raw", 200}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
	}
}

func TestCustomDataDecoderAndEarlyMiddleware(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "context")
	called := 0
	codec := DecodeWith(func(c context.Context, req *Request) (apiData, error) {
		called++
		return apiData{Name: c.Value(contextKey{}).(string) + req.Header.Get("X-Name")}, nil
	})
	s := NewServer()
	deny := func(next Handler) Handler {
		return func(c context.Context, req *Request, resp *Response) error {
			if req.Header.Get("X-Deny") == "yes" {
				return HTTPError{Status: 403}
			}
			return next(c, req, resp)
		}
	}
	if err := s.Group("/api").Use(deny).Register(Post("/", func(ctx context.Context, req RequestOf[apiData]) (string, error) {
		data, err := req.Data(ctx)
		if err != nil {
			return "", err
		}
		return data.Name, nil
	}, WithInput(codec))); err != nil {
		t.Fatal(err)
	}
	for _, blocked := range []bool{true, false} {
		r := httptest.NewRequest("POST", "/api", nil).WithContext(ctx)
		r.Header.Set("X-Name", "data")
		if blocked {
			r.Header.Set("X-Deny", "yes")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if blocked && (w.Code != 403 || called != 0) {
			t.Fatalf("%d calls=%d", w.Code, called)
		}
		if !blocked && (w.Code != 200 || w.Body.String() != "\"contextdata\"\n" || called != 1) {
			t.Fatalf("%d %q calls=%d", w.Code, w.Body.String(), called)
		}
	}
}

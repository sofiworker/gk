package v2

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRequestOfOpenAPIAndParameterDefaults(t *testing.T) {
	s := NewServer().With(WithParameter[int](ParameterQuery, "page", true))
	route := Post("/{id}", func(context.Context, RequestOf[*bodyInputPayload]) (string, error) { return "ok", nil },
		WithInput(DecodeRequest(RequireBody(JSONInput[*bodyInputPayload]()))),
		WithParameter[int64](ParameterPath, "id", false),
		WithParameter[string](ParameterQuery, "page", false))
	if err := s.Group("/users").Register(route); err != nil {
		t.Fatal(err)
	}
	data, err := s.OpenAPI("Body API", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	op := doc["paths"].(map[string]any)["/users/{id}"].(map[string]any)["post"].(map[string]any)
	body := op["requestBody"].(map[string]any)
	if body["required"] != true {
		t.Fatal(body)
	}
	schema := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if schema["$ref"] == nil {
		t.Fatal(schema)
	}
	definitions := doc["components"].(map[string]any)["schemas"].(map[string]any)
	definition := definitions[strings.TrimPrefix(schema["$ref"].(string), "#/components/schemas/")].(map[string]any)
	props := definition["properties"].(map[string]any)
	if len(props) != 1 || props["name"] == nil {
		t.Fatal(props)
	}
	params := op["parameters"].([]any)
	if len(params) != 2 {
		t.Fatal(params)
	}
	for _, item := range params {
		param := item.(map[string]any)
		if param["in"] == "path" && (param["required"] != true || param["schema"].(map[string]any)["type"] != "integer") {
			t.Fatal(param)
		}
		if param["in"] == "query" && (param["required"] != false || param["schema"].(map[string]any)["type"] != "string") {
			t.Fatal(param)
		}
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("POST", "/users/42", strings.NewReader(`{}`)))
	if rec.Code != 200 {
		t.Fatalf("metadata triggered validation: %d", rec.Code)
	}
}

func TestParameterRegistrationErrorsAndOverride(t *testing.T) {
	h := func(context.Context, RequestInput) (string, error) { return "ok", nil }
	for _, option := range []Option{WithParameter[int]("invalid", "x", false), WithParameter[int](ParameterQuery, "", false), WithParameter[int](ParameterPath, "missing", true)} {
		if Get("/{id}", h, option).Err() == nil {
			t.Fatal("invalid parameter accepted")
		}
	}
	r := Get("/{id}", h, WithParameter[string](ParameterHeader, "X-Test", true), WithParameter[int](ParameterHeader, "x-test", false))
	if r.Err() != nil || len(r.parameters) != 1 || r.parameters[0].required {
		t.Fatalf("%v %v", r.Err(), r.parameters)
	}
}

func TestRequestOfMultipartCleanupAndStream(t *testing.T) {
	var payload bytes.Buffer
	w := multipart.NewWriter(&payload)
	part, err := w.CreateFormFile("file", "test.txt")
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
	var temporary string
	r := Post("/", func(_ context.Context, in RequestOf[upload]) (string, error) {
		file, err := in.Data.File.Open()
		if err != nil {
			return "", err
		}
		defer file.Close()
		if disk, ok := file.(*os.File); ok {
			temporary = disk.Name()
		} else {
			t.Fatal("file not spilled")
		}
		return "ok", nil
	}, WithInput(DecodeRequest(MultipartInput[upload](MultipartLimits{MaxBytes: 1024, MemoryBytes: 1}))))
	req := httptest.NewRequest("POST", "/", bytes.NewReader(payload.Bytes()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	if err := r.Serve(context.Background(), &Request{Request: req}, &Response{ResponseWriter: httptest.NewRecorder()}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("temporary file survived: %v", err)
	}
	stream := Post("/", func(_ context.Context, in RequestOf[*multipart.Reader]) (string, error) {
		part, err := in.Data.NextPart()
		if err != nil {
			return "", err
		}
		defer part.Close()
		data, err := io.ReadAll(part)
		return string(data), err
	}, WithInput(DecodeRequest(MultipartStreamInput())))
	req = httptest.NewRequest("POST", "/", bytes.NewReader(payload.Bytes()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	if err := stream.Serve(context.Background(), &Request{Request: req}, &Response{ResponseWriter: rec}); err != nil || rec.Body.String() != "\"content\"\n" {
		t.Fatalf("%v %q", err, rec.Body.String())
	}
}

func BenchmarkRequestOfComposition(b *testing.B) {
	type combined struct {
		ID   string           `path:"id"`
		Page string           `query:"page"`
		Data bodyInputPayload `body:"json"`
	}
	for _, mode := range []string{"DTO", "RequestOf", "ReadBody"} {
		b.Run(mode, func(b *testing.B) {
			s := NewServer()
			var route Route
			switch mode {
			case "DTO":
				route = Post("/{id}", func(_ context.Context, in combined) (string, error) { return in.ID + in.Page + in.Data.Name, nil })
			case "RequestOf":
				route = Post("/{id}", func(_ context.Context, in RequestOf[bodyInputPayload]) (string, error) {
					page, _ := in.QueryFirst("page")
					return in.Path("id") + page + in.Data.Name, nil
				})
			default:
				codec := JSONInput[bodyInputPayload]()
				route = Post("/{id}", func(ctx context.Context, in RequestInput) (string, error) {
					body, err := ReadBody(ctx, in, codec)
					if err != nil {
						return "", err
					}
					page, _ := in.QueryFirst("page")
					return in.Path("id") + page + body.Name, nil
				})
			}
			if err := s.Register(route); err != nil {
				b.Fatal(err)
			}
			preflight := httptest.NewRequest("POST", "/42?page=2", strings.NewReader(`{"name":"alice"}`))
			preflight.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			s.ServeHTTP(recorder, preflight)
			if recorder.Code != 200 || recorder.Body.String() != "\"422alice\"\n" {
				b.Fatalf("%d %q", recorder.Code, recorder.Body.String())
			}
			template := httptest.NewRequest("POST", "/42?page=2", nil)
			template.Header.Set("Content-Type", "application/json")
			writer := &comparisonWriter{}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				request := new(http.Request)
				*request = *template
				request.Body = io.NopCloser(strings.NewReader(`{"name":"alice"}`))
				writer.header = make(http.Header)
				s.ServeHTTP(writer, request)
			}
		})
	}
}

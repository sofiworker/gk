package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rewriteBenchmarkWriter struct {
	header http.Header
}

type rewriteReferenceInput struct {
	ID int64 `path:"id"`
}

type rewriteReferencePayload struct {
	Message string `json:"message"`
	Num     int    `json:"num"`
}

var rewriteReferencePayloadValue = &rewriteReferencePayload{Message: "Hello, World!", Num: 42}

func (w *rewriteBenchmarkWriter) Header() http.Header { return w.header }
func (w *rewriteBenchmarkWriter) WriteHeader(int)     {}
func (w *rewriteBenchmarkWriter) Write(body []byte) (int, error) {
	return len(body), nil
}
func (w *rewriteBenchmarkWriter) WriteString(body string) (int, error) {
	return len(body), nil
}

func rewriteBenchmarkNoop(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(writer, request)
	})
}

func assertRewriteBenchmarkResponse(b *testing.B, handler http.Handler, method, path, contentType, body string) {
	b.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != contentType || recorder.Body.String() != body {
		b.Fatalf("response = %d %q %q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func BenchmarkServerRewriteRawStatic(b *testing.B) {
	server := New(WithProduces(MIMEPlain))
	server.MustMount(RawOperation(http.MethodGet, "/health", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkServerRewriteRawParam(b *testing.B) {
	server := New(WithProduces(MIMEPlain))
	server.MustMount(RawOperation(http.MethodGet, "/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path)
	})))
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkServerRewriteTypedParams(b *testing.B) {
	server := New(WithProduces(MIMEJSON))
	server.MustMount(Handle(Get("/users/{id}"), StructInput[rewriteInput](), JSONOutput[rewriteOutput](), func(_ context.Context, input rewriteInput) (rewriteOutput, error) {
		return rewriteOutput{ID: input.ID, Page: input.Page}, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/users/42?page=3", nil)
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkServerRewriteParamJSONLegacyCodec(b *testing.B) {
	server := New(WithProduces(MIMEJSON))
	server.MustMount(Handle(Get("/user/{id}"), StructInput[rewriteReferenceInput](), JSONOutput[*rewriteReferencePayload](), func(_ context.Context, input rewriteReferenceInput) (*rewriteReferencePayload, error) {
		_ = input.ID
		return rewriteReferencePayloadValue, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/user/42", nil)
	assertRewriteBenchmarkResponse(b, server, http.MethodGet, "/user/42", MIMEJSON, "{\"message\":\"Hello, World!\",\"num\":42}\n")
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationParamJSONReference(b *testing.B) {
	server := New()
	server.MustMount(GetJSON("/user/{id}", PathInt64("id"), func(_ context.Context, id int64) (*rewriteReferencePayload, error) {
		_ = id
		return rewriteReferencePayloadValue, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/user/42", nil)
	assertRewriteBenchmarkResponse(b, server, http.MethodGet, "/user/42", MIMEJSON, "{\"message\":\"Hello, World!\",\"num\":42}")
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationStaticTextReference(b *testing.B) {
	server := New()
	server.MustMount(GetText("/hello", NoInput(), func(context.Context, EmptyInput) (string, error) {
		return "Hello, World!", nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	assertRewriteBenchmarkResponse(b, server, http.MethodGet, "/hello", MIMEPlain, "Hello, World!")
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationStaticTextHandleReference(b *testing.B) {
	server := New()
	server.MustMount(Handle(Get("/hello"), NoInput(), TextOutput(), func(context.Context, EmptyInput) (string, error) {
		return "Hello, World!", nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	assertRewriteBenchmarkResponse(b, server, http.MethodGet, "/hello", MIMEPlain, "Hello, World!")
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationStaticJSONReference(b *testing.B) {
	server := New()
	server.MustMount(GetJSON("/json", NoInput(), func(context.Context, EmptyInput) (*rewriteReferencePayload, error) {
		return rewriteReferencePayloadValue, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	assertRewriteBenchmarkResponse(b, server, http.MethodGet, "/json", MIMEJSON, "{\"message\":\"Hello, World!\",\"num\":42}")
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationStaticJSONHandleReference(b *testing.B) {
	server := New()
	server.MustMount(Handle(Get("/json"), NoInput(), JSONOutput[*rewriteReferencePayload](), func(context.Context, EmptyInput) (*rewriteReferencePayload, error) {
		return rewriteReferencePayloadValue, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	assertRewriteBenchmarkResponse(b, server, http.MethodGet, "/json", MIMEJSON, "{\"message\":\"Hello, World!\",\"num\":42}")
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationParamJSONHandleReference(b *testing.B) {
	server := New()
	server.MustMount(Handle(Get("/user/{id}"), PathInt64("id"), JSONOutput[*rewriteReferencePayload](), func(_ context.Context, id int64) (*rewriteReferencePayload, error) {
		_ = id
		return rewriteReferencePayloadValue, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/user/42", nil)
	assertRewriteBenchmarkResponse(b, server, http.MethodGet, "/user/42", MIMEJSON, "{\"message\":\"Hello, World!\",\"num\":42}")
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationFiveMiddlewareReference(b *testing.B) {
	operation := GetText("/hello", NoInput(), func(context.Context, EmptyInput) (string, error) {
		return "Hello, World!", nil
	}).WithMiddleware(
		rewriteBenchmarkNoop,
		rewriteBenchmarkNoop,
		rewriteBenchmarkNoop,
		rewriteBenchmarkNoop,
		rewriteBenchmarkNoop,
	)
	server := New()
	server.MustMount(operation)
	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	assertRewriteBenchmarkResponse(b, server, http.MethodGet, "/hello", MIMEPlain, "Hello, World!")
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationFiveContextMiddlewareReference(b *testing.B) {
	noop := func(next ContextHandler) ContextHandler {
		return func(c *Context) error { return next(c) }
	}
	operation := GetText("/hello", NoInput(), func(context.Context, EmptyInput) (string, error) {
		return "Hello, World!", nil
	}).WithContextMiddleware(noop, noop, noop, noop, noop)
	server := New()
	server.MustMount(operation)
	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkOperationNotFoundReference(b *testing.B) {
	server := New()
	server.MustMount(GetText("/hello", NoInput(), func(context.Context, EmptyInput) (string, error) {
		return "Hello, World!", nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkServerRewriteMethodNotAllowed(b *testing.B) {
	server := New()
	server.MustMount(RawOperation(http.MethodGet, "/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodPost, "/users/42", nil)
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, req)
	}
}

func BenchmarkServerRewriteJSONBody(b *testing.B) {
	type input struct {
		Body rewriteBody
	}
	server := New(WithProduces(MIMEJSON))
	server.MustMount(Handle(Post("/users"), StructInput[input](), JSONOutput[rewriteOutput](), func(_ context.Context, input input) (rewriteOutput, error) {
		return rewriteOutput{Name: input.Body.Name}, nil
	}))
	body := `{"name":"Ada"}`
	newRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
		req.Header.Set("Content-Type", MIMEJSON)
		return req
	}
	w := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(w, newRequest())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.ServeHTTP(w, newRequest())
	}
}

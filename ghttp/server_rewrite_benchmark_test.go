package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type rewriteBenchmarkWriter struct {
	header http.Header
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

func rewriteBenchmarkNoop(c *Ctx) {
	c.Next()
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

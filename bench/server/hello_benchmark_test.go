package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sofiworker/gk/ghttp"
)

func BenchmarkHelloRawServer(b *testing.B) {
	b.Run("ghttp", benchmarkHelloHandler(newGhttpHelloHandler()))
	b.Run("gin", benchmarkHelloHandler(newGinHelloHandler()))
}

func BenchmarkRouterAllocation(b *testing.B) {
	b.Run("ghttp-static", benchmarkRouterServeHTTP(newGhttpRouter("/hello"), "/hello"))
	b.Run("gin-static", benchmarkRouterServeHTTP(newGinRouter("/hello"), "/hello"))
	b.Run("ghttp-param", benchmarkRouterServeHTTP(newGhttpRouter("/users/{id}"), "/users/42"))
	b.Run("gin-param", benchmarkRouterServeHTTP(newGinRouter("/users/:id"), "/users/42"))
}

func newGhttpHelloHandler() http.Handler {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEPlain))
	ghttp.Route[struct{}, struct{}](s).
		GET("/hello").
		ToHTTP(http.HandlerFunc(ghttpHandler))
	return s
}

func newGinHelloHandler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard

	r := gin.New()
	r.GET("/hello", func(c *gin.Context) {
		handleRequest()
		_, _ = c.Writer.Write(message)
	})
	return r
}

func newGhttpRouter(path string) http.Handler {
	router := ghttp.NewRadixRouter()
	if err := router.Register(http.MethodGet, path, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err != nil {
		panic(err)
	}
	return router
}

func newGinRouter(path string) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard

	r := gin.New()
	r.GET(path, func(*gin.Context) {})
	return r
}

func benchmarkHelloHandler(handler http.Handler) func(*testing.B) {
	return func(b *testing.B) {
		ts := httptest.NewServer(handler)
		defer ts.Close()

		client := ts.Client()
		url := ts.URL + "/hello"

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp, err := client.Get(url)
			if err != nil {
				b.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
}

type discardResponseWriter struct{}

func (discardResponseWriter) Header() http.Header {
	return http.Header{}
}

func (discardResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (discardResponseWriter) WriteHeader(int) {}

func benchmarkRouterServeHTTP(handler http.Handler, path string) func(*testing.B) {
	return func(b *testing.B) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := discardResponseWriter{}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			handler.ServeHTTP(w, req)
		}
	}
}

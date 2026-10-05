package httpmethods

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/labstack/echo/v4"
	"github.com/sofiworker/gk/ghttp"
)

type payload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type result struct {
	Method string `json:"method"`
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Count  int    `json:"count,omitempty"`
}

type benchCase struct {
	name, method, target string
	body                 []byte
}

var cases = []benchCase{
	{name: "GETStatic", method: http.MethodGet, target: "/method/static"},
	{name: "GETPathQueryHeader", method: http.MethodGet, target: "/method/items/42?expand=items&page=2"},
	{name: "POSTJSON", method: http.MethodPost, target: "/method/body", body: []byte(`{"name":"alice","count":3}`)},
	{name: "PUTJSON", method: http.MethodPut, target: "/method/body", body: []byte(`{"name":"alice","count":3}`)},
	{name: "PATCHJSON", method: http.MethodPatch, target: "/method/body", body: []byte(`{"name":"alice","count":3}`)},
	{name: "DELETEPath", method: http.MethodDelete, target: "/method/items/42"},
	{name: "HEAD", method: http.MethodHead, target: "/method/static"},
	{name: "OPTIONS", method: http.MethodOptions, target: "/method/static"},
	{name: "MiddlewareQuery", method: http.MethodGet, target: "/method/middleware?x=1&y=2"},
	{name: "NotFound", method: http.MethodGet, target: "/method/missing"},
}

type discardWriter struct {
	header http.Header
	status int
	n      int
}

func newWriter() *discardWriter              { return &discardWriter{header: make(http.Header)} }
func (w *discardWriter) Header() http.Header { return w.header }
func (w *discardWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *discardWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.n += len(p)
	return len(p), nil
}
func (w *discardWriter) reset() { clear(w.header); w.status, w.n = 0, 0 }

type reusableBody struct {
	data []byte
	off  int
}

func (b *reusableBody) Read(p []byte) (int, error) {
	if b.off == len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.off:])
	b.off += n
	return n, nil
}
func (b *reusableBody) Close() error { return nil }
func (b *reusableBody) reset()       { b.off = 0 }

type target struct {
	name string
	h    http.Handler
}

func valueString(v *ghttp.Value) string {
	s, _ := v.String()
	return s
}

func newRequest(c benchCase) (*http.Request, *reusableBody) {
	u, _ := url.Parse(c.target)
	r := &http.Request{Method: c.method, URL: u, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header), Body: http.NoBody, Host: "bench.local", RequestURI: c.target}
	r.Header.Set("X-Request-ID", "req-1")
	if len(c.body) == 0 {
		return r, nil
	}
	b := &reusableBody{data: c.body}
	r.Body, r.ContentLength = b, int64(len(c.body))
	r.Header.Set("Content-Type", "application/json")
	return r, b
}

func runCase(b *testing.B, t target, c benchCase) {
	r, body := newRequest(c)
	w := newWriter()
	t.h.ServeHTTP(w, r)
	if c.name == "NotFound" {
		if w.status != http.StatusNotFound {
			b.Fatalf("%s %s status=%d", t.name, c.name, w.status)
		}
	} else if w.status < 200 || w.status >= 300 {
		b.Fatalf("%s %s status=%d", t.name, c.name, w.status)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		w.reset()
		if body != nil {
			body.reset()
			r.Body = body
		}
		t.h.ServeHTTP(w, r)
	}
}

func newGinTarget() target {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	jsonOut := func(c *gin.Context, out result) { c.JSON(http.StatusOK, out) }
	r.GET("/method/static", func(c *gin.Context) { jsonOut(c, result{Method: http.MethodGet}) })
	r.GET("/method/items/:id", func(c *gin.Context) {
		jsonOut(c, result{Method: http.MethodGet, ID: c.Param("id") + c.Query("expand") + c.GetHeader("X-Request-ID")})
	})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		r.Handle(method, "/method/body", func(c *gin.Context) {
			var p payload
			if c.ShouldBindJSON(&p) != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			jsonOut(c, result{Method: method, Name: p.Name, Count: p.Count})
		})
	}
	r.DELETE("/method/items/:id", func(c *gin.Context) { jsonOut(c, result{Method: http.MethodDelete, ID: c.Param("id")}) })
	r.HEAD("/method/static", func(c *gin.Context) { c.JSON(http.StatusOK, result{Method: http.MethodHead}) })
	r.OPTIONS("/method/static", func(c *gin.Context) { c.JSON(http.StatusNoContent, nil) })
	mw := func(c *gin.Context) { c.Next() }
	r.GET("/method/middleware", mw, mw, mw, func(c *gin.Context) { jsonOut(c, result{Method: c.Request.Method, ID: c.Query("x") + c.Query("y")}) })
	return target{"gin", r}
}

func newEchoTarget() target {
	e := echo.New()
	jsonOut := func(c echo.Context, out result) error { return c.JSON(http.StatusOK, out) }
	e.GET("/method/static", func(c echo.Context) error { return jsonOut(c, result{Method: http.MethodGet}) })
	e.GET("/method/items/:id", func(c echo.Context) error {
		return jsonOut(c, result{Method: http.MethodGet, ID: c.Param("id") + c.QueryParam("expand") + c.Request().Header.Get("X-Request-ID")})
	})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		e.Add(method, "/method/body", func(c echo.Context) error {
			var p payload
			if c.Bind(&p) != nil {
				return c.NoContent(http.StatusBadRequest)
			}
			return jsonOut(c, result{Method: method, Name: p.Name, Count: p.Count})
		})
	}
	e.DELETE("/method/items/:id", func(c echo.Context) error { return jsonOut(c, result{Method: http.MethodDelete, ID: c.Param("id")}) })
	e.HEAD("/method/static", func(c echo.Context) error { return c.JSON(http.StatusOK, result{Method: http.MethodHead}) })
	e.OPTIONS("/method/static", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	mw := func(next echo.HandlerFunc) echo.HandlerFunc { return func(c echo.Context) error { return next(c) } }
	e.GET("/method/middleware", func(c echo.Context) error {
		return jsonOut(c, result{Method: c.Request().Method, ID: c.QueryParam("x") + c.QueryParam("y")})
	}, mw, mw, mw)
	return target{"echo", e}
}

func newGHTTPTarget() target {
	s := ghttp.NewServer()
	out := func(method string) ghttp.Endpoint[ghttp.NoDataType, result] {
		return func(context.Context, ghttp.RequestOf[ghttp.NoDataType]) (result, error) {
			return result{Method: method}, nil
		}
	}
	_ = s.Register(
		ghttp.Get("/method/static", out(http.MethodGet)),
		ghttp.Get("/method/items/:id", func(_ context.Context, r ghttp.RequestOf[ghttp.NoDataType]) (result, error) {
			q := r.QueryValue("expand")
			return result{Method: http.MethodGet, ID: valueString(r.PathValue("id")) + valueString(&q) + valueString(r.HeaderValue("X-Request-ID"))}, nil
		}),
		ghttp.Delete("/method/items/:id", func(_ context.Context, r ghttp.RequestOf[ghttp.NoDataType]) (result, error) {
			return result{Method: http.MethodDelete, ID: valueString(r.PathValue("id"))}, nil
		}),
		ghttp.Head("/method/static", out(http.MethodHead)),
		ghttp.HandleProcedure(http.MethodOptions, "/method/static", func(context.Context) error { return nil }),
		ghttp.Get("/method/middleware", func(_ context.Context, r ghttp.RequestOf[ghttp.NoDataType]) (result, error) {
			x, _ := r.QueryValue("x").String()
			y, _ := r.QueryValue("y").String()
			return result{Method: http.MethodGet, ID: x + y}, nil
		}),
		ghttp.Post("/method/body", func(_ context.Context, r ghttp.RequestOf[payload]) (result, error) {
			p, err := r.Data(context.Background())
			return result{Method: http.MethodPost, Name: p.Name, Count: p.Count}, err
		}),
		ghttp.Put("/method/body", func(_ context.Context, r ghttp.RequestOf[payload]) (result, error) {
			p, err := r.Data(context.Background())
			return result{Method: http.MethodPut, Name: p.Name, Count: p.Count}, err
		}),
		ghttp.Patch("/method/body", func(_ context.Context, r ghttp.RequestOf[payload]) (result, error) {
			p, err := r.Data(context.Background())
			return result{Method: http.MethodPatch, Name: p.Name, Count: p.Count}, err
		}),
	)
	return target{"ghttp", s}
}

func TestMethodMatrix(t *testing.T) {
	for _, target := range []target{newGHTTPTarget(), newGinTarget(), newEchoTarget()} {
		for _, c := range cases {
			t.Run(target.name+"/"+c.name, func(t *testing.T) {
				r, body := newRequest(c)
				w := newWriter()
				if body != nil {
					body.reset()
					r.Body = body
				}
				target.h.ServeHTTP(w, r)
				want := http.StatusOK
				if c.name == "NotFound" {
					want = http.StatusNotFound
				}
				if c.name == "OPTIONS" {
					want = http.StatusNoContent
				}
				if w.status != want {
					t.Fatalf("status=%d want=%d", w.status, want)
				}
			})
		}
	}
}

func BenchmarkHTTPMethods(b *testing.B) {
	targets := []target{newGHTTPTarget(), newGinTarget(), newEchoTarget()}
	for _, c := range cases {
		for _, target := range targets {
			b.Run(c.name+"/"+target.name, func(b *testing.B) { runCase(b, target, c) })
		}
	}
}

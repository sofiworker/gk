package webbench

// 扩展压力场景:多参数(10)、大规模路由表(1000)、多层中间件(20)。
// ghttp vs gin,注册等价路由、handler 语义一致。
// Extended stress scenarios: many params (10), a large route table (1000),
// and deep middleware (20). ghttp vs gin with equivalent routes and handlers.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sofiworker/gk/ghttp"
)

// ---------------------------------------------------------------------------
// 10 路径参数
// ---------------------------------------------------------------------------

type param10Out struct {
	Count int `json:"count"`
}

type param10Mid struct {
	P1, P2, P3, P4, P5 string
}

type param10Tail struct {
	P6, P7, P8, P9, P10 string
}

type param10Input struct {
	Mid  param10Mid
	Tail param10Tail
}

const param10Path = "/a/{p1}/b/{p2}/c/{p3}/d/{p4}/e/{p5}/f/{p6}/g/{p7}/h/{p8}/i/{p9}/j/{p10}"

const param10RequestPath = "/a/v1/b/v2/c/v3/d/v4/e/v5/f/v6/g/v7/h/v8/i/v9/j/v10"

func newGhttpParam10() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	input := ghttp.MapInputs10(
		ghttp.PathString("p1"), ghttp.PathString("p2"), ghttp.PathString("p3"),
		ghttp.PathString("p4"), ghttp.PathString("p5"), ghttp.PathString("p6"),
		ghttp.PathString("p7"), ghttp.PathString("p8"), ghttp.PathString("p9"),
		ghttp.PathString("p10"),
		func(p1, p2, p3, p4, p5, p6, p7, p8, p9, p10 string) param10Input {
			return param10Input{
				Mid:  param10Mid{P1: p1, P2: p2, P3: p3, P4: p4, P5: p5},
				Tail: param10Tail{P6: p6, P7: p7, P8: p8, P9: p9, P10: p10},
			}
		},
	)
	s.MustMount(ghttp.Handle(ghttp.Get(param10Path), input, ghttp.JSONOutput[param10Out](),
		func(_ context.Context, _ param10Input) (param10Out, error) {
			return param10Out{Count: 10}, nil
		}))
	return s
}

func newGinParam10() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// gin 使用 :param 语法,与 ghttp 的 {param} 一一对应转换。
	// gin uses :param syntax; translate from ghttp's {param}.
	ginPath := strings.ReplaceAll(strings.ReplaceAll(param10Path, "{", ":"), "}", "")
	r.GET(ginPath, func(c *gin.Context) {
		_ = c.Param("p1")
		_ = c.Param("p2")
		_ = c.Param("p3")
		_ = c.Param("p4")
		_ = c.Param("p5")
		_ = c.Param("p6")
		_ = c.Param("p7")
		_ = c.Param("p8")
		_ = c.Param("p9")
		_ = c.Param("p10")
		c.JSON(200, param10Out{Count: 10})
	})
	return r
}

// ---------------------------------------------------------------------------
// 1000 条路由
// ---------------------------------------------------------------------------

const scale1000Resources = 250 // 250 × 4 = 1000 条

func newGhttpScale1000() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	for i := 0; i < scale1000Resources; i++ {
		base := fmt.Sprintf("/api/v2/res%d", i)
		s.MustMount(
			ghttp.GetJSON(base, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			}),
			ghttp.GetJSON(base+"/{id}", ghttp.PathString("id"), func(_ context.Context, id string) (ghttpIDOut, error) {
				return ghttpIDOut{ID: id}, nil
			}),
			ghttp.PostJSON(base, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			}),
			ghttp.PutJSON(base+"/{id}", ghttp.PathString("id"), func(_ context.Context, id string) (ghttpIDOut, error) {
				return ghttpIDOut{ID: id}, nil
			}),
		)
	}
	return s
}

func newGinScale1000() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	for i := 0; i < scale1000Resources; i++ {
		base := fmt.Sprintf("/api/v2/res%d", i)
		r.GET(base, func(c *gin.Context) { c.JSON(200, pingOut{Message: "ok"}) })
		r.GET(base+"/:id", func(c *gin.Context) { c.JSON(200, ghttpIDOut{ID: c.Param("id")}) })
		r.POST(base, func(c *gin.Context) { c.JSON(200, pingOut{Message: "ok"}) })
		r.PUT(base+"/:id", func(c *gin.Context) { c.JSON(200, ghttpIDOut{ID: c.Param("id")}) })
	}
	return r
}

// ---------------------------------------------------------------------------
// 20 层中间件
// ---------------------------------------------------------------------------

const middleware20Count = 20

func newGhttpMiddleware20() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	group := s.Group("/mw20")
	for i := 0; i < middleware20Count; i++ {
		group.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		})
	}
	group.MustMount(ghttp.GetJSON("/ping", ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
		return pingOut{Message: "pong"}, nil
	}))
	return s
}

func newGinMiddleware20() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	group := r.Group("/mw20")
	for i := 0; i < middleware20Count; i++ {
		group.Use(func(c *gin.Context) {})
	}
	group.GET("/ping", func(c *gin.Context) { c.JSON(200, pingOut{Message: "pong"}) })
	return r
}

// ---------------------------------------------------------------------------
// 场景注册与 sanity
// ---------------------------------------------------------------------------

func TestExtendedScenarioSanity(t *testing.T) {
	probe := func(h http.Handler, method, path string, body []byte) (int, string) {
		var reader io.Reader = http.NoBody
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req := httptest.NewRequest(method, path, reader)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	for _, target := range []string{"ghttp", "gin"} {
		t.Run(target, func(t *testing.T) {
			var param10, scale1000, mw20 http.Handler
			if target == "ghttp" {
				param10 = newGhttpParam10()
				scale1000 = newGhttpScale1000()
				mw20 = newGhttpMiddleware20()
			} else {
				param10 = newGinParam10()
				scale1000 = newGinScale1000()
				mw20 = newGinMiddleware20()
			}
			if code, body := probe(param10, http.MethodGet, param10RequestPath, nil); code != 200 || body == "" {
				t.Fatalf("param10 = %d %q", code, body)
			}
			if code, _ := probe(scale1000, http.MethodGet, "/api/v2/res99", nil); code != 200 {
				t.Fatalf("scale1000 static = %d", code)
			}
			if code, _ := probe(scale1000, http.MethodGet, "/api/v2/res99/777", nil); code != 200 {
				t.Fatalf("scale1000 param = %d", code)
			}
			if code, _ := probe(scale1000, http.MethodGet, "/api/v2/res249/1", nil); code != 200 {
				t.Fatalf("scale1000 tail = %d", code)
			}
			if code, _ := probe(mw20, http.MethodGet, "/mw20/ping", nil); code != 200 {
				t.Fatalf("middleware20 = %d", code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 基准
// ---------------------------------------------------------------------------

func benchExtended(b *testing.B, h http.Handler, method, path string, body []byte) {
	var br *bytes.Reader
	if body != nil {
		br = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, nil)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := newMockWriter()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if br != nil {
			br.Reset(body)
			req.Body = io.NopCloser(br)
			req.ContentLength = int64(len(body))
		}
		h.ServeHTTP(w, req)
	}
}

func BenchmarkParam10(b *testing.B) {
	b.Run("ghttp", func(b *testing.B) { benchExtended(b, newGhttpParam10(), http.MethodGet, param10RequestPath, nil) })
	b.Run("gin", func(b *testing.B) { benchExtended(b, newGinParam10(), http.MethodGet, param10RequestPath, nil) })
}

func BenchmarkScale1000(b *testing.B) {
	paths := []string{
		"/api/v2/res0",
		"/api/v2/res12/77",
		"/api/v2/res99",
		"/api/v2/res99/777",
		"/api/v2/res249/1",
		"/api/v2/res200/42",
	}
	b.Run("ghttp", func(b *testing.B) {
		h := newGhttpScale1000()
		b.ReportAllocs()
		b.ResetTimer()
		i := 0
		w := newMockWriter()
		for b.Loop() {
			req := httptest.NewRequest(http.MethodGet, paths[i%len(paths)], nil)
			h.ServeHTTP(w, req)
			i++
		}
	})
	b.Run("gin", func(b *testing.B) {
		h := newGinScale1000()
		b.ReportAllocs()
		b.ResetTimer()
		i := 0
		w := newMockWriter()
		for b.Loop() {
			req := httptest.NewRequest(http.MethodGet, paths[i%len(paths)], nil)
			h.ServeHTTP(w, req)
			i++
		}
	})
}

func BenchmarkMiddleware20(b *testing.B) {
	b.Run("ghttp", func(b *testing.B) { benchExtended(b, newGhttpMiddleware20(), http.MethodGet, "/mw20/ping", nil) })
	b.Run("gin", func(b *testing.B) { benchExtended(b, newGinMiddleware20(), http.MethodGet, "/mw20/ping", nil) })
}

// ---------------------------------------------------------------------------
// 20 路径参数(极端场景)
// ---------------------------------------------------------------------------

func param20PathAndRequest() (routePath, requestPath string) {
	routePath = "/p"
	requestPath = "/p"
	for i := 1; i <= 20; i++ {
		routePath += fmt.Sprintf("/s%d/{p%d}", i, i)
		requestPath += fmt.Sprintf("/s%d/v%d", i, i)
	}
	return routePath, requestPath
}

func newGhttpParam20() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	routePath, _ := param20PathAndRequest()
	input := ghttp.MapInputs(
		ghttp.MapInputs10(
			ghttp.PathString("p1"), ghttp.PathString("p2"), ghttp.PathString("p3"),
			ghttp.PathString("p4"), ghttp.PathString("p5"), ghttp.PathString("p6"),
			ghttp.PathString("p7"), ghttp.PathString("p8"), ghttp.PathString("p9"),
			ghttp.PathString("p10"),
			func(p1, p2, p3, p4, p5, p6, p7, p8, p9, p10 string) [10]string {
				return [10]string{p1, p2, p3, p4, p5, p6, p7, p8, p9, p10}
			},
		),
		ghttp.MapInputs10(
			ghttp.PathString("p11"), ghttp.PathString("p12"), ghttp.PathString("p13"),
			ghttp.PathString("p14"), ghttp.PathString("p15"), ghttp.PathString("p16"),
			ghttp.PathString("p17"), ghttp.PathString("p18"), ghttp.PathString("p19"),
			ghttp.PathString("p20"),
			func(p11, p12, p13, p14, p15, p16, p17, p18, p19, p20 string) [10]string {
				return [10]string{p11, p12, p13, p14, p15, p16, p17, p18, p19, p20}
			},
		),
		func(a, b [10]string) param10Out {
			return param10Out{Count: 20}
		},
	)
	s.MustMount(ghttp.Handle(ghttp.Get(routePath), input, ghttp.JSONOutput[param10Out](),
		func(_ context.Context, _ param10Out) (param10Out, error) {
			return param10Out{Count: 20}, nil
		}))
	return s
}

func newGinParam20() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	routePath, _ := param20PathAndRequest()
	ginPath := strings.ReplaceAll(strings.ReplaceAll(routePath, "{", ":"), "}", "")
	r.GET(ginPath, func(c *gin.Context) {
		for i := 1; i <= 20; i++ {
			_ = c.Param(fmt.Sprintf("p%d", i))
		}
		c.JSON(200, param10Out{Count: 20})
	})
	return r
}

// ---------------------------------------------------------------------------
// 5000 条路由
// ---------------------------------------------------------------------------

const scale5000Resources = 1250 // 1250 × 4 = 5000 条

func newGhttpScale5000() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	for i := 0; i < scale5000Resources; i++ {
		base := fmt.Sprintf("/api/v3/res%d", i)
		s.MustMount(
			ghttp.GetJSON(base, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			}),
			ghttp.GetJSON(base+"/{id}", ghttp.PathString("id"), func(_ context.Context, id string) (ghttpIDOut, error) {
				return ghttpIDOut{ID: id}, nil
			}),
			ghttp.PostJSON(base, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			}),
			ghttp.PutJSON(base+"/{id}", ghttp.PathString("id"), func(_ context.Context, id string) (ghttpIDOut, error) {
				return ghttpIDOut{ID: id}, nil
			}),
		)
	}
	return s
}

func newGinScale5000() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	for i := 0; i < scale5000Resources; i++ {
		base := fmt.Sprintf("/api/v3/res%d", i)
		r.GET(base, func(c *gin.Context) { c.JSON(200, pingOut{Message: "ok"}) })
		r.GET(base+"/:id", func(c *gin.Context) { c.JSON(200, ghttpIDOut{ID: c.Param("id")}) })
		r.POST(base, func(c *gin.Context) { c.JSON(200, pingOut{Message: "ok"}) })
		r.PUT(base+"/:id", func(c *gin.Context) { c.JSON(200, ghttpIDOut{ID: c.Param("id")}) })
	}
	return r
}

// ---------------------------------------------------------------------------
// 10 层中间件(斜率采样)
// ---------------------------------------------------------------------------

func newGhttpMiddleware10() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	group := s.Group("/mw10")
	for i := 0; i < 10; i++ {
		group.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		})
	}
	group.MustMount(ghttp.GetJSON("/ping", ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
		return pingOut{Message: "pong"}, nil
	}))
	return s
}

func newGinMiddleware10() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	group := r.Group("/mw10")
	for i := 0; i < 10; i++ {
		group.Use(func(c *gin.Context) {})
	}
	group.GET("/ping", func(c *gin.Context) { c.JSON(200, pingOut{Message: "pong"}) })
	return r
}

func BenchmarkParam20(b *testing.B) {
	_, requestPath := param20PathAndRequest()
	b.Run("ghttp", func(b *testing.B) { benchExtended(b, newGhttpParam20(), http.MethodGet, requestPath, nil) })
	b.Run("gin", func(b *testing.B) { benchExtended(b, newGinParam20(), http.MethodGet, requestPath, nil) })
}

func BenchmarkScale5000(b *testing.B) {
	paths := []string{
		"/api/v3/res0",
		"/api/v3/res12/77",
		"/api/v3/res499",
		"/api/v3/res1249/1",
		"/api/v3/res1000/42",
	}
	for _, fw := range []struct {
		name  string
		build func() http.Handler
	}{
		{"ghttp", func() http.Handler { return newGhttpScale5000() }},
		{"gin", func() http.Handler { return newGinScale5000() }},
	} {
		b.Run(fw.name, func(b *testing.B) {
			h := fw.build()
			b.ReportAllocs()
			b.ResetTimer()
			i := 0
			w := newMockWriter()
			for b.Loop() {
				req := httptest.NewRequest(http.MethodGet, paths[i%len(paths)], nil)
				h.ServeHTTP(w, req)
				i++
			}
		})
	}
}

func BenchmarkMiddleware10(b *testing.B) {
	b.Run("ghttp", func(b *testing.B) { benchExtended(b, newGhttpMiddleware10(), http.MethodGet, "/mw10/ping", nil) })
	b.Run("gin", func(b *testing.B) { benchExtended(b, newGinMiddleware10(), http.MethodGet, "/mw10/ping", nil) })
}

// ---------------------------------------------------------------------------
// 极端中间件深度与有负载中间件
// ---------------------------------------------------------------------------

func newGhttpMiddlewareN(n int, work bool) *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	group := s.Group(fmt.Sprintf("/mwn%d", n))
	for i := 0; i < n; i++ {
		idx := i
		group.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if work {
					w.Header().Set(fmt.Sprintf("X-MW-%d", idx), "v")
				}
				next.ServeHTTP(w, r)
			})
		})
	}
	group.MustMount(ghttp.GetJSON("/ping", ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
		return pingOut{Message: "pong"}, nil
	}))
	return s
}

func newGinMiddlewareN(n int, work bool) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	group := r.Group(fmt.Sprintf("/mwn%d", n))
	for i := 0; i < n; i++ {
		idx := i
		group.Use(func(c *gin.Context) {
			if work {
				c.Header(fmt.Sprintf("X-MW-%d", idx), "v")
			}
		})
	}
	group.GET("/ping", func(c *gin.Context) { c.JSON(200, pingOut{Message: "pong"}) })
	return r
}

func BenchmarkMiddleware50(b *testing.B) {
	b.Run("ghttp", func(b *testing.B) {
		benchExtended(b, newGhttpMiddlewareN(50, false), http.MethodGet, "/mwn50/ping", nil)
	})
	b.Run("gin", func(b *testing.B) { benchExtended(b, newGinMiddlewareN(50, false), http.MethodGet, "/mwn50/ping", nil) })
}

func BenchmarkMiddleware100(b *testing.B) {
	b.Run("ghttp", func(b *testing.B) {
		benchExtended(b, newGhttpMiddlewareN(100, false), http.MethodGet, "/mwn100/ping", nil)
	})
	b.Run("gin", func(b *testing.B) {
		benchExtended(b, newGinMiddlewareN(100, false), http.MethodGet, "/mwn100/ping", nil)
	})
}

func BenchmarkMiddlewareWork20(b *testing.B) {
	b.Run("ghttp", func(b *testing.B) {
		benchExtended(b, newGhttpMiddlewareN(20, true), http.MethodGet, "/mwn20/ping", nil)
	})
	b.Run("gin", func(b *testing.B) { benchExtended(b, newGinMiddlewareN(20, true), http.MethodGet, "/mwn20/ping", nil) })
}

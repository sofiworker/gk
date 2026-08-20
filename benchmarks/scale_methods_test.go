package webbench

// 规模化方法/请求体/查询参数横评:ghttp vs gin vs echo vs web vs stdmux。
// Large-scale methods, request bodies, and query params: ghttp vs gin vs echo
// vs web vs stdmux.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	web "example.com/web"
	"github.com/gin-gonic/gin"
	"github.com/labstack/echo/v4"
	"github.com/sofiworker/gk/ghttp"
)

// ---------------------------------------------------------------------------
// 1 路径 × 7 方法:方法分发规模化
// ---------------------------------------------------------------------------

// methods7Path 是多方法路由的共享路径。
const methods7Path = "/api/v1/items"

type methods7Out struct {
	Method string `json:"method"`
}

var methods7Methods = []string{
	"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD",
}

func newGhttpMethods7() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.MustMount(
		ghttp.GetJSON(methods7Path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
			return methods7Out{Method: "GET"}, nil
		}),
		ghttp.PostJSON(methods7Path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
			return methods7Out{Method: "POST"}, nil
		}),
		ghttp.PutJSON(methods7Path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
			return methods7Out{Method: "PUT"}, nil
		}),
		ghttp.PatchJSON(methods7Path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
			return methods7Out{Method: "PATCH"}, nil
		}),
		ghttp.DeleteJSON(methods7Path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
			return methods7Out{Method: "DELETE"}, nil
		}),
		ghttp.Handle(ghttp.Options(methods7Path), ghttp.NoInput(), ghttp.NoContentOutput[struct{}](), func(_ context.Context, _ ghttp.EmptyInput) (struct{}, error) {
			return struct{}{}, nil
		}),
		ghttp.Handle(ghttp.Head(methods7Path), ghttp.NoInput(), ghttp.NoContentOutput[struct{}](), func(_ context.Context, _ ghttp.EmptyInput) (struct{}, error) {
			return struct{}{}, nil
		}),
	)
	return s
}

func newGinMethods7() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET(methods7Path, func(c *gin.Context) { c.JSON(200, methods7Out{Method: "GET"}) })
	r.POST(methods7Path, func(c *gin.Context) { c.JSON(200, methods7Out{Method: "POST"}) })
	r.PUT(methods7Path, func(c *gin.Context) { c.JSON(200, methods7Out{Method: "PUT"}) })
	r.PATCH(methods7Path, func(c *gin.Context) { c.JSON(200, methods7Out{Method: "PATCH"}) })
	r.DELETE(methods7Path, func(c *gin.Context) { c.JSON(200, methods7Out{Method: "DELETE"}) })
	r.OPTIONS(methods7Path, func(c *gin.Context) { c.Status(204) })
	r.HEAD(methods7Path, func(c *gin.Context) { c.Status(204) })
	return r
}

func newEchoMethods7() *echo.Echo {
	e := echo.New()
	e.GET(methods7Path, func(c echo.Context) error { return c.JSON(200, methods7Out{Method: "GET"}) })
	e.POST(methods7Path, func(c echo.Context) error { return c.JSON(200, methods7Out{Method: "POST"}) })
	e.PUT(methods7Path, func(c echo.Context) error { return c.JSON(200, methods7Out{Method: "PUT"}) })
	e.PATCH(methods7Path, func(c echo.Context) error { return c.JSON(200, methods7Out{Method: "PATCH"}) })
	e.DELETE(methods7Path, func(c echo.Context) error { return c.JSON(200, methods7Out{Method: "DELETE"}) })
	e.OPTIONS(methods7Path, func(c echo.Context) error { return c.NoContent(204) })
	e.HEAD(methods7Path, func(c echo.Context) error { return c.NoContent(204) })
	return e
}

func newWebMethods7() *web.App {
	s := web.New()
	s.Must(
		web.GetJSON(methods7Path, web.NoIn(), func(web.None) (methods7Out, error) {
			return methods7Out{Method: "GET"}, nil
		}),
		web.PostJSON(methods7Path, web.NoIn(), func(web.None) (methods7Out, error) {
			return methods7Out{Method: "POST"}, nil
		}),
		web.PutJSON(methods7Path, web.NoIn(), func(web.None) (methods7Out, error) {
			return methods7Out{Method: "PUT"}, nil
		}),
		web.PatchJSON(methods7Path, web.NoIn(), func(web.None) (methods7Out, error) {
			return methods7Out{Method: "PATCH"}, nil
		}),
		web.DeleteJSON(methods7Path, web.NoIn(), func(web.None) (methods7Out, error) {
			return methods7Out{Method: "DELETE"}, nil
		}),
		web.Handle(web.Options(methods7Path), web.NoIn(), web.NoContent[web.None](),
			func(web.None) (web.None, error) { return web.None{}, nil }),
		web.Handle(web.Head(methods7Path), web.NoIn(), web.NoContent[web.None](),
			func(web.None) (web.None, error) { return web.None{}, nil }),
	)
	return s
}

func BenchmarkMethods7(b *testing.B) {
	servers := []struct {
		name string
		h    http.Handler
	}{
		{"ghttp", newGhttpMethods7()},
		{"gin", newGinMethods7()},
		{"echo", newEchoMethods7()},
		{"web", newWebMethods7()},
	}
	for _, srv := range servers {
		b.Run(srv.name, func(b *testing.B) {
			w := newMockWriter()
			b.ReportAllocs()
			b.ResetTimer()
			i := 0
			for b.Loop() {
				req := httptest.NewRequest(methods7Methods[i%7], methods7Path, nil)
				srv.h.ServeHTTP(w, req)
				i++
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 200 路径 × 7 方法 = 1400 路由:命中最后一条路径,遍历所有方法
// ---------------------------------------------------------------------------

const methodsScaleCount = 200 // 200 paths

func newGhttpMethodsScale() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	for i := 0; i < methodsScaleCount; i++ {
		path := fmt.Sprintf("/api/v3/res%d", i)
		s.MustMount(
			ghttp.GetJSON(path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
				return methods7Out{Method: "GET"}, nil
			}),
			ghttp.PostJSON(path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
				return methods7Out{Method: "POST"}, nil
			}),
			ghttp.PutJSON(path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
				return methods7Out{Method: "PUT"}, nil
			}),
			ghttp.PatchJSON(path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
				return methods7Out{Method: "PATCH"}, nil
			}),
			ghttp.DeleteJSON(path, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (methods7Out, error) {
				return methods7Out{Method: "DELETE"}, nil
			}),
			ghttp.Handle(ghttp.Options(path), ghttp.NoInput(), ghttp.NoContentOutput[struct{}](), func(_ context.Context, _ ghttp.EmptyInput) (struct{}, error) {
				return struct{}{}, nil
			}),
			ghttp.Handle(ghttp.Head(path), ghttp.NoInput(), ghttp.NoContentOutput[struct{}](), func(_ context.Context, _ ghttp.EmptyInput) (struct{}, error) {
				return struct{}{}, nil
			}),
		)
	}
	return s
}

func newGinMethodsScale() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	for i := 0; i < methodsScaleCount; i++ {
		path := fmt.Sprintf("/api/v3/res%d", i)
		h := func(method string) gin.HandlerFunc {
			return func(c *gin.Context) {
				c.JSON(200, methods7Out{Method: method})
			}
		}
		r.GET(path, h("GET"))
		r.POST(path, h("POST"))
		r.PUT(path, h("PUT"))
		r.PATCH(path, h("PATCH"))
		r.DELETE(path, h("DELETE"))
		r.OPTIONS(path, func(c *gin.Context) { c.Status(204) })
		r.HEAD(path, func(c *gin.Context) { c.Status(204) })
	}
	return r
}

func newEchoMethodsScale() *echo.Echo {
	e := echo.New()
	for i := 0; i < methodsScaleCount; i++ {
		path := fmt.Sprintf("/api/v3/res%d", i)
		h := func(method string) echo.HandlerFunc {
			return func(c echo.Context) error {
				return c.JSON(200, methods7Out{Method: method})
			}
		}
		e.GET(path, h("GET"))
		e.POST(path, h("POST"))
		e.PUT(path, h("PUT"))
		e.PATCH(path, h("PATCH"))
		e.DELETE(path, h("DELETE"))
		e.OPTIONS(path, func(c echo.Context) error { return c.NoContent(204) })
		e.HEAD(path, func(c echo.Context) error { return c.NoContent(204) })
	}
	return e
}

func BenchmarkMethodsScale1400(b *testing.B) {
	servers := []struct {
		name string
		h    http.Handler
	}{
		{"ghttp", newGhttpMethodsScale()},
		{"gin", newGinMethodsScale()},
		{"echo", newEchoMethodsScale()},
	}
	lastPath := fmt.Sprintf("/api/v3/res%d", methodsScaleCount-1)
	for _, srv := range servers {
		b.Run(srv.name, func(b *testing.B) {
			w := newMockWriter()
			b.ReportAllocs()
			b.ResetTimer()
			i := 0
			for b.Loop() {
				req := httptest.NewRequest(methods7Methods[i%7], lastPath, nil)
				srv.h.ServeHTTP(w, req)
				i++
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 100KB JSON 请求体
// ---------------------------------------------------------------------------

type largeBodyIn struct {
	Data []byte `json:"data"`
}

type largeBodyOut struct {
	Size int `json:"size"`
}

// buildBodyOfSize 构造指定原始字节数(编码后因 base64 膨胀约 1.33×)的 JSON
// 请求体。Data 填 i%256 保证内容不可压缩、解码路径完整执行。
// buildBodyOfSize builds a JSON body whose decoded payload is size bytes (the
// encoded body is ~1.33x larger due to base64). The i%256 fill keeps the
// content incompressible so the decode path runs in full.
func buildBodyOfSize(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	body, _ := json.Marshal(largeBodyIn{Data: data})
	return body
}

func newGhttpBodyLarge() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.MustMount(ghttp.Handle(
		ghttp.Post("/api/large"),
		ghttp.JSONBody[largeBodyIn](),
		ghttp.JSONOutput[largeBodyOut](),
		func(_ context.Context, in largeBodyIn) (largeBodyOut, error) {
			return largeBodyOut{Size: len(in.Data)}, nil
		},
	))
	return s
}

func newGinBodyLarge() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/api/large", func(c *gin.Context) {
		var in largeBodyIn
		body, _ := io.ReadAll(c.Request.Body)
		_ = json.Unmarshal(body, &in)
		c.JSON(200, largeBodyOut{Size: len(in.Data)})
	})
	return r
}

func newEchoBodyLarge() *echo.Echo {
	e := echo.New()
	e.POST("/api/large", func(c echo.Context) error {
		var in largeBodyIn
		body, _ := io.ReadAll(c.Request().Body)
		_ = json.Unmarshal(body, &in)
		return c.JSON(200, largeBodyOut{Size: len(in.Data)})
	})
	return e
}

func newWebBodyLarge() *web.App {
	s := web.New()
	s.Must(web.Handle(
		web.Post("/api/large"),
		web.BodyJSON[largeBodyIn](),
		web.JSON[largeBodyOut](),
		func(in largeBodyIn) (largeBodyOut, error) {
			return largeBodyOut{Size: len(in.Data)}, nil
		},
	))
	return s
}

func BenchmarkBodyLarge(b *testing.B) {
	// 请求体阶梯:1KB/10KB/100KB 原始字节(base64 编码后约 1.3KB/13KB/133KB)。
	// 验证各框架在大请求体上的时间与分配是否线性增长。
	// body ladder: 1KB/10KB/100KB raw bytes (1.3KB/13KB/133KB after base64).
	// Verifies time and allocation growth stay linear for large bodies.
	bodyLadder := []struct {
		name string
		body []byte
	}{
		{"1KB", buildBodyOfSize(1 * 1024)},
		{"10KB", buildBodyOfSize(10 * 1024)},
		{"100KB", buildBodyOfSize(100 * 1024)},
	}
	servers := []struct {
		name string
		h    http.Handler
	}{
		{"ghttp", newGhttpBodyLarge()},
		{"gin", newGinBodyLarge()},
		{"echo", newEchoBodyLarge()},
		{"web", newWebBodyLarge()},
	}
	for _, step := range bodyLadder {
		for _, srv := range servers {
			b.Run(step.name+"/"+srv.name, func(b *testing.B) {
				w := newMockWriter()
				body := step.body
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					req := httptest.NewRequest("POST", "/api/large", bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.ContentLength = int64(len(body))
					srv.h.ServeHTTP(w, req)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// 10 个查询参数
// ---------------------------------------------------------------------------

type query10Out struct {
	Q1  string `json:"q1"`
	Q2  string `json:"q2"`
	Q3  string `json:"q3"`
	Q4  string `json:"q4"`
	Q5  string `json:"q5"`
	Q6  string `json:"q6"`
	Q7  string `json:"q7"`
	Q8  string `json:"q8"`
	Q9  string `json:"q9"`
	Q10 string `json:"q10"`
}

const query10Path = "/api/query?s=v1&s=v2&s=v3&s=v4&s=v5&s=v6&s=v7&s=v8&s=v9&s=v10"

func newGhttpQuery10() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	input := ghttp.QueryStrings("s")
	s.MustMount(ghttp.Handle(
		ghttp.Get("/api/query"), input, ghttp.JSONOutput[query10Out](),
		func(_ context.Context, values []string) (query10Out, error) {
			out := query10Out{}
			if len(values) > 0 {
				out.Q1 = values[0]
			}
			if len(values) > 1 {
				out.Q2 = values[1]
			}
			if len(values) > 2 {
				out.Q3 = values[2]
			}
			if len(values) > 3 {
				out.Q4 = values[3]
			}
			if len(values) > 4 {
				out.Q5 = values[4]
			}
			if len(values) > 5 {
				out.Q6 = values[5]
			}
			if len(values) > 6 {
				out.Q7 = values[6]
			}
			if len(values) > 7 {
				out.Q8 = values[7]
			}
			if len(values) > 8 {
				out.Q9 = values[8]
			}
			if len(values) > 9 {
				out.Q10 = values[9]
			}
			return out, nil
		},
	))
	return s
}

func newGinQuery10() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/api/query", func(c *gin.Context) {
		values := c.QueryArray("s")
		out := query10Out{}
		if len(values) > 0 {
			out.Q1 = values[0]
		}
		if len(values) > 1 {
			out.Q2 = values[1]
		}
		if len(values) > 2 {
			out.Q3 = values[2]
		}
		if len(values) > 3 {
			out.Q4 = values[3]
		}
		if len(values) > 4 {
			out.Q5 = values[4]
		}
		if len(values) > 5 {
			out.Q6 = values[5]
		}
		if len(values) > 6 {
			out.Q7 = values[6]
		}
		if len(values) > 7 {
			out.Q8 = values[7]
		}
		if len(values) > 8 {
			out.Q9 = values[8]
		}
		if len(values) > 9 {
			out.Q10 = values[9]
		}
		c.JSON(200, out)
	})
	return r
}

func newEchoQuery10() *echo.Echo {
	e := echo.New()
	e.GET("/api/query", func(c echo.Context) error {
		values := c.QueryParams()["s"]
		out := query10Out{}
		if len(values) > 0 {
			out.Q1 = values[0]
		}
		if len(values) > 1 {
			out.Q2 = values[1]
		}
		if len(values) > 2 {
			out.Q3 = values[2]
		}
		if len(values) > 3 {
			out.Q4 = values[3]
		}
		if len(values) > 4 {
			out.Q5 = values[4]
		}
		if len(values) > 5 {
			out.Q6 = values[5]
		}
		if len(values) > 6 {
			out.Q7 = values[6]
		}
		if len(values) > 7 {
			out.Q8 = values[7]
		}
		if len(values) > 8 {
			out.Q9 = values[8]
		}
		if len(values) > 9 {
			out.Q10 = values[9]
		}
		return c.JSON(200, out)
	})
	return e
}

func newWebQuery10() *web.App {
	s := web.New()
	input := web.QueryStrings("s")
	s.Must(web.Handle(
		web.Get("/api/query"), input, web.JSON[query10Out](),
		func(values []string) (query10Out, error) {
			out := query10Out{}
			if len(values) > 0 {
				out.Q1 = values[0]
			}
			if len(values) > 1 {
				out.Q2 = values[1]
			}
			if len(values) > 2 {
				out.Q3 = values[2]
			}
			if len(values) > 3 {
				out.Q4 = values[3]
			}
			if len(values) > 4 {
				out.Q5 = values[4]
			}
			if len(values) > 5 {
				out.Q6 = values[5]
			}
			if len(values) > 6 {
				out.Q7 = values[6]
			}
			if len(values) > 7 {
				out.Q8 = values[7]
			}
			if len(values) > 8 {
				out.Q9 = values[8]
			}
			if len(values) > 9 {
				out.Q10 = values[9]
			}
			return out, nil
		},
	))
	return s
}

func BenchmarkQueryParams10(b *testing.B) {
	servers := []struct {
		name string
		h    http.Handler
	}{
		{"ghttp", newGhttpQuery10()},
		{"gin", newGinQuery10()},
		{"echo", newEchoQuery10()},
		{"web", newWebQuery10()},
	}
	for _, srv := range servers {
		b.Run(srv.name, func(b *testing.B) {
			w := newMockWriter()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				req := httptest.NewRequest("GET", query10Path, nil)
				srv.h.ServeHTTP(w, req)
			}
		})
	}
}

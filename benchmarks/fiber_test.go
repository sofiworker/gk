package webbench

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// gofiber/fiber v3 — fasthttp based, so it cannot be driven through
// http.Handler. Benchmarks call the fasthttp handler directly with reusable
// fasthttp.RequestCtx values (the approach used by go-web-framework-benchmark);
// the sanity probe uses the official app.Test.
//
// Fairness note: fiber skips net/http request/response conversion entirely,
// which is also exactly its production fast path.

func newFiber() *fiber.App {
	app := fiber.New()

	app.Get("/ping", func(c fiber.Ctx) error {
		return c.SendString("pong")
	})

	app.Get("/users/:id", func(c fiber.Ctx) error {
		return c.SendString(c.Params("id"))
	})

	app.Get("/orgs/:org/teams/:team/members/:member/roles/:role/perms/:perm", func(c fiber.Ctx) error {
		return c.SendString(c.Params("org") + c.Params("team") + c.Params("member") +
			c.Params("role") + c.Params("perm"))
	})

	app.Get("/files/*", func(c fiber.Ctx) error {
		return c.SendString(c.Params("*"))
	})

	app.Get("/search", func(c fiber.Ctx) error {
		return c.SendString(c.Query("q") + c.Query("page") + c.Query("limit"))
	})

	app.Post("/users", func(c fiber.Ctx) error {
		var in userIn
		if err := c.Bind().Body(&in); err != nil {
			return c.Status(http.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(makeUserOut(in))
	})

	app.Get("/profile", func(c fiber.Ctx) error {
		return c.JSON(profileFixture)
	})

	mws := make([]any, 0, middlewareCount)
	for i := 0; i < middlewareCount; i++ {
		mws = append(mws, fiber.Handler(func(c fiber.Ctx) error { return c.Next() }))
	}
	mw := app.Group("/mw", mws...)
	mw.Get("/ping", func(c fiber.Ctx) error {
		return c.SendString("pong")
	})

	app.Put("/api/v1/users/:id/orders", func(c fiber.Ctx) error {
		var in orderIn
		if err := c.Bind().Body(&in); err != nil {
			return c.Status(http.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(makeOrderOut(
			c.Params("id"), c.Query("expand"), c.Query("currency"),
			c.Get("X-Request-ID"), in,
		))
	})

	idHandler := func(c fiber.Ctx) error {
		return c.SendString(c.Params("id"))
	}
	okHandler := func(c fiber.Ctx) error {
		return c.SendString("ok")
	}
	for _, rt := range scaleRoutes() {
		pattern := rt.pattern
		if rt.hasID {
			pattern = pattern[:len(pattern)-len("{id}")] + ":id"
		}
		h := okHandler
		if rt.hasID {
			h = idHandler
		}
		switch rt.method {
		case "GET":
			app.Get(pattern, h)
		case "POST":
			app.Post(pattern, h)
		case "PUT":
			app.Put(pattern, h)
		}
	}

	return app
}

type fiberTarget struct {
	app *fiber.App
	h   fasthttp.RequestHandler
}

func (t *fiberTarget) name() string { return "fiber" }

func buildFiberCtxs(sc scenario) []*fasthttp.RequestCtx {
	ctxs := make([]*fasthttp.RequestCtx, 0, len(sc.targets))
	for _, tgt := range sc.targets {
		var req fasthttp.Request
		req.Header.SetMethod(sc.method)
		req.SetRequestURI(tgt)
		for k, v := range sc.headers {
			req.Header.Set(k, v)
		}
		if len(sc.body) > 0 {
			req.Header.SetContentType("application/json")
			req.SetBody(sc.body)
		}
		ctx := &fasthttp.RequestCtx{}
		ctx.Init(&req, nil, nil)
		ctxs = append(ctxs, ctx)
	}
	return ctxs
}

func (t *fiberTarget) bench(b *testing.B, sc scenario) {
	ctxs := buildFiberCtxs(sc)
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		ctx := ctxs[i%len(ctxs)]
		ctx.Response.Reset()
		t.h(ctx)
		i++
	}
}

func (t *fiberTarget) benchParallel(b *testing.B, sc scenario) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctxs := buildFiberCtxs(sc)
		i := 0
		for pb.Next() {
			ctx := ctxs[i%len(ctxs)]
			ctx.Response.Reset()
			t.h(ctx)
			i++
		}
	})
}

func (t *fiberTarget) probe(sc scenario) (int, string, error) {
	var body io.Reader
	if len(sc.body) > 0 {
		body = bytes.NewReader(sc.body)
	}
	req := httptest.NewRequest(sc.method, sc.targets[0], body)
	for k, v := range sc.headers {
		req.Header.Set(k, v)
	}
	if len(sc.body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.app.Test(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(data), nil
}

func init() {
	app := newFiber()
	register(&fiberTarget{app: app, h: app.Handler()}, nil)
}

package webbench

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
)

// cloudwego/hertz — ByteDance's netpoll-based framework. Driven in-process
// via the official ut.PerformRequest helper (no network), which allocates a
// response recorder per request; that constant overhead is included in its
// numbers and noted in the README.

func newHertz() *route.Engine {
	hlog.SetLevel(hlog.LevelError) // silence per-route registration debug logs
	engine := route.NewEngine(config.NewOptions([]config.Option{}))

	engine.GET("/ping", func(ctx context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, "pong")
	})

	engine.GET("/users/:id", func(ctx context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, c.Param("id"))
	})

	engine.GET("/orgs/:org/teams/:team/members/:member/roles/:role/perms/:perm",
		func(ctx context.Context, c *app.RequestContext) {
			c.String(consts.StatusOK, c.Param("org")+c.Param("team")+c.Param("member")+
				c.Param("role")+c.Param("perm"))
		})

	engine.GET("/files/*path", func(ctx context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, c.Param("path"))
	})

	engine.GET("/search", func(ctx context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, c.Query("q")+c.Query("page")+c.Query("limit"))
	})

	engine.POST("/users", func(ctx context.Context, c *app.RequestContext) {
		var in userIn
		if err := c.Bind(&in); err != nil {
			c.String(consts.StatusBadRequest, err.Error())
			return
		}
		c.JSON(consts.StatusOK, makeUserOut(in))
	})

	engine.GET("/profile", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, profileFixture)
	})

	mws := make([]app.HandlerFunc, 0, middlewareCount)
	for i := 0; i < middlewareCount; i++ {
		mws = append(mws, func(ctx context.Context, c *app.RequestContext) {
			c.Next(ctx)
		})
	}
	mw := engine.Group("/mw", mws...)
	mw.GET("/ping", func(ctx context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, "pong")
	})

	engine.PUT("/api/v1/users/:id/orders", func(ctx context.Context, c *app.RequestContext) {
		var in orderIn
		if err := c.Bind(&in); err != nil {
			c.String(consts.StatusBadRequest, err.Error())
			return
		}
		c.JSON(consts.StatusOK, makeOrderOut(
			c.Param("id"), c.Query("expand"), c.Query("currency"),
			c.Request.Header.Get("X-Request-ID"), in,
		))
	})

	idHandler := func(ctx context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, c.Param("id"))
	}
	okHandler := func(ctx context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, "ok")
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
		engine.Handle(rt.method, pattern, h)
	}

	return engine
}

type hertzTarget struct {
	engine *route.Engine
}

func (t *hertzTarget) name() string { return "hertz" }

func hertzHeaders(sc scenario) []ut.Header {
	hs := make([]ut.Header, 0, len(sc.headers)+1)
	for k, v := range sc.headers {
		hs = append(hs, ut.Header{Key: k, Value: v})
	}
	if len(sc.body) > 0 {
		hs = append(hs, ut.Header{Key: "Content-Type", Value: "application/json"})
	}
	return hs
}

func (t *hertzTarget) bench(b *testing.B, sc scenario) {
	headers := hertzHeaders(sc)
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		target := sc.targets[i%len(sc.targets)]
		var body *ut.Body
		if len(sc.body) > 0 {
			body = &ut.Body{Body: bytes.NewReader(sc.body), Len: len(sc.body)}
		}
		ut.PerformRequest(t.engine, sc.method, target, body, headers...)
		i++
	}
}

func (t *hertzTarget) benchParallel(b *testing.B, sc scenario) {
	headers := hertzHeaders(sc)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			target := sc.targets[i%len(sc.targets)]
			var body *ut.Body
			if len(sc.body) > 0 {
				body = &ut.Body{Body: bytes.NewReader(sc.body), Len: len(sc.body)}
			}
			ut.PerformRequest(t.engine, sc.method, target, body, headers...)
			i++
		}
	})
}

func (t *hertzTarget) probe(sc scenario) (int, string, error) {
	var body *ut.Body
	if len(sc.body) > 0 {
		body = &ut.Body{Body: bytes.NewReader(sc.body), Len: len(sc.body)}
	}
	w := ut.PerformRequest(t.engine, sc.method, sc.targets[0], body, hertzHeaders(sc)...)
	resp := w.Result()
	return resp.StatusCode(), string(resp.Body()), nil
}

func init() {
	register(&hertzTarget{engine: newHertz()}, nil)
}

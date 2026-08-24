// ghttp - the typed generics HTTP server (this repository). 本仓库的泛型 typed HTTP 服务器。

package webbench

import (
	"context"
	"net/http"
	"strconv"

	"github.com/sofiworker/gk/ghttp"
)

// ghttp is this project's own Typed Server v4 — gin-style middleware, unified
// Engine/Server type, radix router. All routes register with typed entries that
// return JSON outputs; for static and param scenarios we emit a minimal pingOut
// struct so the benchmark measures ghttp's full request path without special-casing.

// ---- 输出类型 / Output types ----

type gHttpPingOut struct {
	Message string `json:"message"`
}

type gHttpIDOut struct {
	ID string `json:"id"`
}

type gHttpParam5Out struct {
	Org    string `json:"org"`
	Team   string `json:"team"`
	Member string `json:"member"`
	Role   string `json:"role"`
	Perm   string `json:"perm"`
}

type gHttpQueryOut struct {
	Q     string `json:"q"`
	Page  string `json:"page"`
	Limit string `json:"limit"`
}

type gHttpMWOut struct {
	Message string `json:"message"`
}

// ---- 输入参数 / Input parameters ----

type gHttpSmall struct {
	ID      int64  `path:"id"`
	Keyword string `query:"keyword"`
	Page    int    `query:"page"`
}

type gHttpLarge struct {
	ID      int64  `path:"id"`
	Keyword string `query:"keyword"`
	Page    int    `query:"page"`
	Size    int    `query:"size"`
	Sort    string `query:"sort"`
	Desc    bool   `query:"desc"`
	Trace   string `header:"X-Trace"`
	Tenant  string `header:"X-Tenant"`
	Region  string `header:"X-Region"`
}

type gHttpEmptyIn struct{} // placeholder for route registrations that don't need params

// ---- 路由构建 / Route building ----

func newGHttp() http.Handler {
	m := ghttp.New()

	// static: GET /ping → 200 OK + {"message":"pong"}
	ghttp.GetNone(
		m, "/ping", ghttp.JSON[gHttpPingOut](),
		func(context.Context) (gHttpPingOut, error) {
			return gHttpPingOut{Message: "pong"}, nil
		},
	)

	// param1: GET /users/{id} → 200 OK + {"id":"12345"}
	ghttp.GetParams(
		m, "/users/{id}", ghttp.JSON[gHttpIDOut](),
		func(ctx context.Context, p gHttpSmall) (gHttpIDOut, error) {
			return gHttpIDOut{ID: strconv.FormatInt(p.ID, 10)}, nil
		},
	)

	// param5: deep path
	ghttp.GetParams(
		m, "/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}",
		ghttp.JSON[gHttpParam5Out](),
		func(ctx context.Context, p gHttpLarge) (gHttpParam5Out, error) {
			return gHttpParam5Out{
				Org:    strconv.FormatInt(p.ID, 10),
				Team:   strconv.Itoa(p.Page),
				Member: strconv.Itoa(p.Size),
				Role:   p.Sort,
				Perm:   "read",
			}, nil
		},
	)

	// wildcard: catch-all
	ghttp.GetParams(
		m, "/files/{path...}", ghttp.JSON[gHttpIDOut](),
		func(ctx context.Context, p gHttpSmall) (gHttpIDOut, error) {
			return gHttpIDOut{ID: p.Keyword}, nil
		},
	)

	// query: 3 query params
	ghttp.GetParams(
		m, "/search", ghttp.JSON[gHttpQueryOut](),
		func(ctx context.Context, p gHttpSmall) (gHttpQueryOut, error) {
			return gHttpQueryOut{Q: p.Keyword, Page: strconv.Itoa(p.Page), Limit: "50"}, nil
		},
	)

	// json bind: POST /users + body → 200 OK + userOut
	ghttp.PostBody(
		m, "/users", ghttp.JSONBody(), ghttp.JSON[userOut](),
		func(ctx context.Context, in userIn) (userOut, error) {
			return makeUserOut(in), nil
		},
	)

	// json response: GET /profile → 200 OK + profile fixture
	ghttp.GetNone(
		m, "/profile", ghttp.JSON[profile](),
		func(context.Context) (profile, error) {
			return profileFixture, nil
		},
	)

	// middleware x5: group with noop chain
	mw := m.Group("")
	for i := 0; i < middlewareCount; i++ {
		mw.Use(ghttp.Middleware(func(next ghttp.Handler) ghttp.Handler {
			return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
				return next(ctx, req, resp)
			}
		}))
	}
	ghttp.GetNone(mw, "/mw/ping", ghttp.JSON[gHttpPingOut](),
		func(context.Context) (gHttpPingOut, error) {
			return gHttpPingOut{Message: "pong"}, nil
		},
	)

	// full chain: PUT /api/v1/users/{id}/orders + path + 2 query + header + body → 200 OK
	ghttp.PutParamsBody(
		m, "/api/v1/users/{id}/orders",
		ghttp.JSONBody(), ghttp.JSON[orderOut](),
		func(ctx context.Context, p gHttpLarge, in orderIn) (orderOut, error) {
			return makeOrderOut(strconv.FormatInt(p.ID, 10), p.Keyword, strconv.Itoa(p.Page), p.Trace, in), nil
		},
	)

	// scale routes: 200 registered (GET list, GET item, POST list, PUT item)
	for _, rt := range scaleRoutes() {
		pattern := curlyPath(rt.pattern)
		if rt.hasID {
			switch rt.method {
			case "GET":
				ghttp.GetParams(m, pattern, ghttp.JSON[gHttpIDOut](),
					func(ctx context.Context, s gHttpSmall) (gHttpIDOut, error) {
						return gHttpIDOut{ID: strconv.FormatInt(s.ID, 10)}, nil
					},
				)
			case "PUT":
				ghttp.PutParams(m, pattern, ghttp.JSON[gHttpIDOut](),
					func(ctx context.Context, s gHttpSmall) (gHttpIDOut, error) {
						return gHttpIDOut{ID: strconv.FormatInt(s.ID, 10)}, nil
					},
				)
			}
			continue
		}
		switch rt.method {
		case "GET":
			ghttp.GetNone(m, pattern, ghttp.JSON[pingOut](), func(context.Context) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			})
		case "POST":
			// no-body case: use PostParams with empty struct as placeholder
			ghttp.PostParams(m, pattern, ghttp.JSON[pingOut](), func(ctx context.Context, e gHttpEmptyIn) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			})
		}
	}

	return m
}

func init() {
	register(&httpTarget{n: "ghttp", h: newGHttp()}, nil)
}

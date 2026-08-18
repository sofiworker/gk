package webbench

import (
	web "example.com/web"
)

// web is the endpoint-as-data framework under test (/root/test): routes are
// first-class typed values, handlers are plain functions, zero reflection.
// Measured with its default chain: no middleware, no problem+json.

type webIDOut struct {
	ID string `json:"id"`
}

type webParam5Out struct {
	Org    string `json:"org"`
	Team   string `json:"team"`
	Member string `json:"member"`
	Role   string `json:"role"`
	Perm   string `json:"perm"`
}

type webQueryOut struct {
	Q     string `json:"q"`
	Page  string `json:"page"`
	Limit string `json:"limit"`
}

type webParam5Mid struct {
	Org    string
	Team   string
	Member string
}

type webParam5Tail struct {
	Role string
	Perm string
}

type webChainMeta struct {
	Expand    string
	Currency  string
	RequestID string
}

type webChainBody struct {
	UserID string
	Body   orderIn
}

type webOrderInput struct {
	Meta   webChainMeta
	UserID string
	Body   orderIn
}

func webNoop(next web.Handler) web.Handler {
	return func(c *web.Ctx) error { return next(c) }
}

func newWebServer() *web.App {
	app := web.New()

	app.Must(
		// static
		web.GetJSON("/ping", web.NoIn(), func(web.None) (pingOut, error) {
			return pingOut{Message: "pong"}, nil
		}),

		// param1
		web.GetJSON("/users/{id}", web.PathString("id"), func(id string) (webIDOut, error) {
			return webIDOut{ID: id}, nil
		}),
	)

	// param5: 5 path params composed with the framework's mappers.
	app.Must(web.Handle(
		web.Get("/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}"),
		web.MapIn(
			web.MapIn3(
				web.PathString("org"), web.PathString("team"), web.PathString("member"),
				func(org, team, member string) webParam5Mid {
					return webParam5Mid{Org: org, Team: team, Member: member}
				},
			),
			web.MapIn(
				web.PathString("role"), web.PathString("perm"),
				func(role, perm string) webParam5Tail {
					return webParam5Tail{Role: role, Perm: perm}
				},
			),
			func(mid webParam5Mid, tail webParam5Tail) webParam5Out {
				return webParam5Out{
					Org: mid.Org, Team: mid.Team, Member: mid.Member,
					Role: tail.Role, Perm: tail.Perm,
				}
			},
		),
		web.JSON[webParam5Out](),
		func(in webParam5Out) (webParam5Out, error) { return in, nil },
	))

	// wildcard
	app.Must(web.Handle(
		web.Get("/files/{path...}"),
		web.PathRest("path"), web.JSON[webIDOut](),
		func(path string) (webIDOut, error) { return webIDOut{ID: path}, nil },
	))

	// query params (3)
	app.Must(web.Handle(
		web.Get("/search"),
		web.MapIn3(
			web.QueryString("q"), web.QueryString("page"), web.QueryString("limit"),
			func(q, page, limit string) webQueryOut {
				return webQueryOut{Q: q, Page: page, Limit: limit}
			},
		),
		web.JSON[webQueryOut](),
		func(in webQueryOut) (webQueryOut, error) { return in, nil },
	))

	// json bind
	app.Must(web.Handle(
		web.Post("/users"),
		web.BodyJSON[userIn](), web.JSON[userOut](),
		func(in userIn) (userOut, error) { return makeUserOut(in), nil },
	))

	// json response
	app.Must(web.Handle(
		web.Get("/profile"),
		web.NoIn(), web.JSON[profile](),
		func(web.None) (profile, error) { return profileFixture, nil },
	))

	// middleware x5 (group scoped so other routes stay clean)
	mws := make([]web.Middleware, 0, middlewareCount)
	for i := 0; i < middlewareCount; i++ {
		mws = append(mws, webNoop)
	}
	g := app.Group("/mw", mws...)
	g.Must(web.GetJSON("/ping", web.NoIn(), func(web.None) (pingOut, error) {
		return pingOut{Message: "pong"}, nil
	}))

	// full chain: path + 2 query + 1 header + JSON body
	app.Must(web.Handle(
		web.Put("/api/v1/users/{id}/orders"),
		web.MapIn(
			web.MapIn3(
				web.QueryString("expand"), web.QueryString("currency"), web.HeaderString("X-Request-ID"),
				func(expand, currency, requestID string) webChainMeta {
					return webChainMeta{Expand: expand, Currency: currency, RequestID: requestID}
				},
			),
			web.MapIn(
				web.PathString("id"), web.BodyJSON[orderIn](),
				func(id string, body orderIn) webChainBody {
					return webChainBody{UserID: id, Body: body}
				},
			),
			func(meta webChainMeta, body webChainBody) webOrderInput {
				return webOrderInput{Meta: meta, UserID: body.UserID, Body: body.Body}
			},
		),
		web.JSON[orderOut](),
		func(in webOrderInput) (orderOut, error) {
			return makeOrderOut(in.UserID, in.Meta.Expand, in.Meta.Currency, in.Meta.RequestID, in.Body), nil
		},
	))

	// route scale: 200 routes
	for _, r := range scaleRoutes() {
		pattern := r.pattern // web uses {id} natively
		if r.hasID {
			switch r.method {
			case "GET":
				app.Must(web.GetJSON(pattern, web.PathString("id"), func(id string) (webIDOut, error) {
					return webIDOut{ID: id}, nil
				}))
			case "PUT":
				app.Must(web.PutJSON(pattern, web.PathString("id"), func(id string) (webIDOut, error) {
					return webIDOut{ID: id}, nil
				}))
			}
			continue
		}
		switch r.method {
		case "GET":
			app.Must(web.GetJSON(pattern, web.NoIn(), func(web.None) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			}))
		case "POST":
			app.Must(web.PostJSON(pattern, web.NoIn(), func(web.None) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			}))
		}
	}

	return app
}

func init() {
	register(&httpTarget{n: "web", h: newWebServer()}, nil)
}

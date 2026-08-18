package webbench

import (
	"context"
	"net/http"

	"github.com/sofiworker/gk/ghttp"
)

// ghttp (this repository). Typed Operation pipeline with the compiled router.
//
// Envelope is opt-in (WithEnvelope) and off by default, so payloads carry the
// same JSON bytes as the other net/http frameworks. Measured with its default
// chain: no server middleware, no validator.

type ghttpIDOut struct {
	ID string `json:"id"`
}

type ghttpParam5Out struct {
	Org    string `json:"org"`
	Team   string `json:"team"`
	Member string `json:"member"`
	Role   string `json:"role"`
	Perm   string `json:"perm"`
}

type ghttpQueryOut struct {
	Q     string `json:"q"`
	Page  string `json:"page"`
	Limit string `json:"limit"`
}

type ghttpChainMeta struct {
	Expand    string
	Currency  string
	RequestID string
}

type ghttpOrderInput struct {
	Meta   ghttpChainMeta
	UserID string
	Body   orderIn
}

func newGhttpServer() *ghttp.Server {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))

	// static
	s.MustMount(ghttp.GetJSON("/ping", ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
		return pingOut{Message: "pong"}, nil
	}))

	// param1
	s.MustMount(ghttp.GetJSON("/users/{id}", ghttp.PathString("id"), func(_ context.Context, id string) (ghttpIDOut, error) {
		return ghttpIDOut{ID: id}, nil
	}))

	// param5: 5 path params composed with the framework's mappers.
	param5 := ghttp.MapInputs5(
		ghttp.PathString("org"), ghttp.PathString("team"), ghttp.PathString("member"),
		ghttp.PathString("role"), ghttp.PathString("perm"),
		func(org, team, member, role, perm string) ghttpParam5Out {
			return ghttpParam5Out{Org: org, Team: team, Member: member, Role: role, Perm: perm}
		},
	)
	s.MustMount(ghttp.Handle(
		ghttp.Get("/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}"),
		param5, ghttp.JSONOutput[ghttpParam5Out](),
		func(_ context.Context, in ghttpParam5Out) (ghttpParam5Out, error) { return in, nil },
	))

	// wildcard
	s.MustMount(ghttp.Handle(
		ghttp.Get("/files/{path...}"),
		ghttp.PathRemainder("path"), ghttp.JSONOutput[ghttpIDOut](),
		func(_ context.Context, path string) (ghttpIDOut, error) { return ghttpIDOut{ID: path}, nil },
	))

	// query params (3)
	query := ghttp.MapInputs3(
		ghttp.QueryString("q"), ghttp.QueryString("page"), ghttp.QueryString("limit"),
		func(q, page, limit string) ghttpQueryOut {
			return ghttpQueryOut{Q: q, Page: page, Limit: limit}
		},
	)
	s.MustMount(ghttp.Handle(
		ghttp.Get("/search"),
		query, ghttp.JSONOutput[ghttpQueryOut](),
		func(_ context.Context, in ghttpQueryOut) (ghttpQueryOut, error) { return in, nil },
	))

	// json bind
	s.MustMount(ghttp.Handle(
		ghttp.Post("/users"),
		ghttp.JSONBody[userIn](), ghttp.JSONOutput[userOut](),
		func(_ context.Context, in userIn) (userOut, error) { return makeUserOut(in), nil },
	))

	// json response
	s.MustMount(ghttp.Handle(
		ghttp.Get("/profile"),
		ghttp.NoInput(), ghttp.JSONOutput[profile](),
		func(_ context.Context, _ ghttp.EmptyInput) (profile, error) { return profileFixture, nil },
	))

	// middleware x5 (group scoped so other routes stay clean)
	mw := s.Group("/mw")
	for i := 0; i < middlewareCount; i++ {
		mw.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		})
	}
	mw.MustMount(ghttp.GetJSON("/ping", ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
		return pingOut{Message: "pong"}, nil
	}))

	// full chain: path + 2 query + 1 header + JSON body
	fullChain := ghttp.MapInputs5(
		ghttp.PathString("id"), ghttp.QueryString("expand"), ghttp.QueryString("currency"),
		ghttp.HeaderString("X-Request-ID"), ghttp.JSONBody[orderIn](),
		func(id, expand, currency, requestID string, body orderIn) ghttpOrderInput {
			return ghttpOrderInput{UserID: id, Meta: ghttpChainMeta{Expand: expand, Currency: currency, RequestID: requestID}, Body: body}
		},
	)
	s.MustMount(ghttp.Handle(
		ghttp.Put("/api/v1/users/{id}/orders"),
		fullChain, ghttp.JSONOutput[orderOut](),
		func(_ context.Context, in ghttpOrderInput) (orderOut, error) {
			return makeOrderOut(in.UserID, in.Meta.Expand, in.Meta.Currency, in.Meta.RequestID, in.Body), nil
		},
	))

	// route scale: 200 routes
	for _, r := range scaleRoutes() {
		pattern := r.pattern // ghttp uses {id} natively
		if r.hasID {
			switch r.method {
			case "GET":
				s.MustMount(ghttp.GetJSON(pattern, ghttp.PathString("id"), func(_ context.Context, id string) (ghttpIDOut, error) {
					return ghttpIDOut{ID: id}, nil
				}))
			case "PUT":
				s.MustMount(ghttp.Handle(
					ghttp.Put(pattern), ghttp.PathString("id"), ghttp.JSONOutput[ghttpIDOut](),
					func(_ context.Context, id string) (ghttpIDOut, error) { return ghttpIDOut{ID: id}, nil },
				))
			}
			continue
		}
		switch r.method {
		case "GET":
			s.MustMount(ghttp.GetJSON(pattern, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			}))
		case "POST":
			s.MustMount(ghttp.PostJSON(pattern, ghttp.NoInput(), func(_ context.Context, _ ghttp.EmptyInput) (pingOut, error) {
				return pingOut{Message: "ok"}, nil
			}))
		}
	}

	return s
}

func init() {
	register(&httpTarget{n: "ghttp", h: newGhttpServer()}, nil)
}

package webbench

import (
	"context"
	"net/http"

	"github.com/sofiworker/gk/ghttp"
)

// ghttp (this repository). Two variants: the default RadixRouter and the
// Go 1.22+ ServeMux-based StdRouter.
//
// Note: ghttp's idiomatic response is the typed-handler pipeline with the
// default JSON envelope ({code,msg,data}), so its responses carry a small,
// constant extra JSON wrapper compared to bare frameworks. That is part of
// its default request chain and is measured as such.

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

type ghttpUserBindIn struct {
	ghttp.Params `json:"-"`

	Body userIn
}

type ghttpOrderIn struct {
	ghttp.Params `json:"-"`

	Body orderIn
}

func newGhttpServer(router ghttp.Router) *ghttp.Server {
	opts := []ghttp.ServerOption{ghttp.WithProduces(ghttp.MIMEJSON)}
	if router != nil {
		opts = append(opts, ghttp.WithRouter(router))
	}
	s := ghttp.New(opts...)

	// static
	ghttp.Route[ghttp.Params, pingOut](s).GET("/ping").
		To(func(ctx context.Context, _ ghttp.Params) (pingOut, error) {
			return pingOut{Message: "pong"}, nil
		})

	// param1
	ghttp.Route[ghttp.Params, ghttpIDOut](s).GET("/users/{id}").
		To(func(ctx context.Context, p ghttp.Params) (ghttpIDOut, error) {
			return ghttpIDOut{ID: p.Path("id")}, nil
		})

	// param5
	ghttp.Route[ghttp.Params, ghttpParam5Out](s).
		GET("/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}").
		To(func(ctx context.Context, p ghttp.Params) (ghttpParam5Out, error) {
			return ghttpParam5Out{
				Org:    p.Path("org"),
				Team:   p.Path("team"),
				Member: p.Path("member"),
				Role:   p.Path("role"),
				Perm:   p.Path("perm"),
			}, nil
		})

	// wildcard
	ghttp.Route[ghttp.Params, ghttpIDOut](s).GET("/files/{path...}").
		To(func(ctx context.Context, p ghttp.Params) (ghttpIDOut, error) {
			return ghttpIDOut{ID: p.Path("path")}, nil
		})

	// query params
	ghttp.Route[ghttp.Params, ghttpQueryOut](s).GET("/search").
		To(func(ctx context.Context, p ghttp.Params) (ghttpQueryOut, error) {
			return ghttpQueryOut{
				Q:     p.Query("q"),
				Page:  p.Query("page"),
				Limit: p.Query("limit"),
			}, nil
		})

	// json bind
	ghttp.Route[ghttpUserBindIn, userOut](s).POST("/users").
		To(func(ctx context.Context, in ghttpUserBindIn) (userOut, error) {
			return makeUserOut(in.Body), nil
		})

	// json response
	ghttp.Route[ghttp.Params, profile](s).GET("/profile").
		To(func(ctx context.Context, _ ghttp.Params) (profile, error) {
			return profileFixture, nil
		})

	// middleware x5 (group scoped so other routes stay clean)
	mw := s.Group("/mw")
	for i := 0; i < middlewareCount; i++ {
		mw.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		})
	}
	ghttp.Route[ghttp.Params, pingOut](mw).GET("/ping").
		To(func(ctx context.Context, _ ghttp.Params) (pingOut, error) {
			return pingOut{Message: "pong"}, nil
		})

	// full chain: path + query + header + body
	ghttp.Route[ghttpOrderIn, orderOut](s).PUT("/api/v1/users/{id}/orders").
		To(func(ctx context.Context, in ghttpOrderIn) (orderOut, error) {
			return makeOrderOut(
				in.Path("id"),
				in.Query("expand"),
				in.Query("currency"),
				in.Header("X-Request-ID"),
				in.Body,
			), nil
		})

	// route scale: 200 routes
	for _, r := range scaleRoutes() {
		pattern := r.pattern // ghttp uses {id} natively
		if r.hasID {
			switch r.method {
			case "GET":
				ghttp.Route[ghttp.Params, ghttpIDOut](s).GET(pattern).
					To(func(ctx context.Context, p ghttp.Params) (ghttpIDOut, error) {
						return ghttpIDOut{ID: p.Path("id")}, nil
					})
			case "PUT":
				ghttp.Route[ghttp.Params, ghttpIDOut](s).PUT(pattern).
					To(func(ctx context.Context, p ghttp.Params) (ghttpIDOut, error) {
						return ghttpIDOut{ID: p.Path("id")}, nil
					})
			}
			continue
		}
		switch r.method {
		case "GET":
			ghttp.Route[ghttp.Params, pingOut](s).GET(pattern).
				To(func(ctx context.Context, _ ghttp.Params) (pingOut, error) {
					return pingOut{Message: "ok"}, nil
				})
		case "POST":
			ghttp.Route[ghttp.Params, pingOut](s).POST(pattern).
				To(func(ctx context.Context, _ ghttp.Params) (pingOut, error) {
					return pingOut{Message: "ok"}, nil
				})
		}
	}

	return s
}

func init() {
	register(&httpTarget{n: "ghttp-radix", h: newGhttpServer(nil)}, nil)
	register(&httpTarget{n: "ghttp-std", h: newGhttpServer(ghttp.NewStdRouter())}, nil)
}

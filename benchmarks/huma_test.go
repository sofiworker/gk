package webbench

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// danielgtaylor/huma v2 — modern OpenAPI-first framework, running here on the
// Go 1.22+ stdlib ServeMux via the humago adapter. Huma validates inputs
// against generated JSON Schema as part of its designed pipeline, so its
// per-request work is intentionally larger than bare routers.

type humaPingOut struct {
	Body pingOut
}

type humaIDOut struct {
	Body struct {
		ID string `json:"id"`
	}
}

func humaID(id string) *humaIDOut {
	out := &humaIDOut{}
	out.Body.ID = id
	return out
}

func newHuma() http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("webbench", "1.0.0"))

	huma.Register(api, huma.Operation{
		OperationID: "ping", Method: http.MethodGet, Path: "/ping",
	}, func(ctx context.Context, _ *struct{}) (*humaPingOut, error) {
		return &humaPingOut{Body: pingOut{Message: "pong"}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-user", Method: http.MethodGet, Path: "/users/{id}",
	}, func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*humaIDOut, error) {
		return humaID(in.ID), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "param5", Method: http.MethodGet,
		Path: "/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}",
	}, func(ctx context.Context, in *struct {
		Org    string `path:"org"`
		Team   string `path:"team"`
		Member string `path:"member"`
		Role   string `path:"role"`
		Perm   string `path:"perm"`
	}) (*humaIDOut, error) {
		return humaID(in.Org + in.Team + in.Member + in.Role + in.Perm), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "wildcard", Method: http.MethodGet, Path: "/files/{path...}",
	}, func(ctx context.Context, in *struct {
		Path string `path:"path"`
	}) (*humaIDOut, error) {
		return humaID(in.Path), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "search", Method: http.MethodGet, Path: "/search",
	}, func(ctx context.Context, in *struct {
		Q     string `query:"q"`
		Page  string `query:"page"`
		Limit string `query:"limit"`
	}) (*humaIDOut, error) {
		return humaID(in.Q + in.Page + in.Limit), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-user", Method: http.MethodPost, Path: "/users",
	}, func(ctx context.Context, in *struct {
		Body userIn
	}) (*struct{ Body userOut }, error) {
		return &struct{ Body userOut }{Body: makeUserOut(in.Body)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "profile", Method: http.MethodGet, Path: "/profile",
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body profile }, error) {
		return &struct{ Body profile }{Body: profileFixture}, nil
	})

	mws := make(huma.Middlewares, 0, middlewareCount)
	for i := 0; i < middlewareCount; i++ {
		mws = append(mws, func(ctx huma.Context, next func(huma.Context)) {
			next(ctx)
		})
	}
	huma.Register(api, huma.Operation{
		OperationID: "mw-ping", Method: http.MethodGet, Path: "/mw/ping",
		Middlewares: mws,
	}, func(ctx context.Context, _ *struct{}) (*humaPingOut, error) {
		return &humaPingOut{Body: pingOut{Message: "pong"}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "put-order", Method: http.MethodPut, Path: "/api/v1/users/{id}/orders",
	}, func(ctx context.Context, in *struct {
		ID        string `path:"id"`
		Expand    string `query:"expand"`
		Currency  string `query:"currency"`
		RequestID string `header:"X-Request-ID"`
		Body      orderIn
	}) (*struct{ Body orderOut }, error) {
		return &struct{ Body orderOut }{
			Body: makeOrderOut(in.ID, in.Expand, in.Currency, in.RequestID, in.Body),
		}, nil
	})

	for i, rt := range scaleRoutes() {
		opID := fmt.Sprintf("scale-%d-%s", i, rt.method)
		if rt.hasID {
			huma.Register(api, huma.Operation{
				OperationID: opID, Method: rt.method, Path: rt.pattern,
			}, func(ctx context.Context, in *struct {
				ID string `path:"id"`
			}) (*humaIDOut, error) {
				return humaID(in.ID), nil
			})
		} else {
			huma.Register(api, huma.Operation{
				OperationID: opID, Method: rt.method, Path: rt.pattern,
			}, func(ctx context.Context, _ *struct{}) (*humaIDOut, error) {
				return humaID("ok"), nil
			})
		}
	}

	return mux
}

func init() {
	register(&httpTarget{n: "huma", h: newHuma()}, nil)
}

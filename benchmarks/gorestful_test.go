package webbench

import (
	"net/http"

	restful "github.com/emicklei/go-restful/v3"
)

// emicklei/go-restful v3 — the veteran resource-oriented REST framework
// (used by Kubernetes apiserver).

func newGoRestful() http.Handler {
	container := restful.NewContainer()

	ws := new(restful.WebService)
	ws.Path("/").
		Consumes(restful.MIME_JSON).
		Produces(restful.MIME_JSON)

	ws.Route(ws.GET("/ping").To(func(req *restful.Request, resp *restful.Response) {
		_, _ = resp.Write([]byte("pong"))
	}))

	ws.Route(ws.GET("/users/{id}").To(func(req *restful.Request, resp *restful.Response) {
		_, _ = resp.Write([]byte(req.PathParameter("id")))
	}))

	ws.Route(ws.GET("/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}").
		To(func(req *restful.Request, resp *restful.Response) {
			_, _ = resp.Write([]byte(req.PathParameter("org") + req.PathParameter("team") +
				req.PathParameter("member") + req.PathParameter("role") + req.PathParameter("perm")))
		}))

	ws.Route(ws.GET("/files/{path:*}").To(func(req *restful.Request, resp *restful.Response) {
		_, _ = resp.Write([]byte(req.PathParameter("path")))
	}))

	ws.Route(ws.GET("/search").To(func(req *restful.Request, resp *restful.Response) {
		_, _ = resp.Write([]byte(req.QueryParameter("q") + req.QueryParameter("page") +
			req.QueryParameter("limit")))
	}))

	ws.Route(ws.POST("/users").To(func(req *restful.Request, resp *restful.Response) {
		var in userIn
		if err := req.ReadEntity(&in); err != nil {
			_ = resp.WriteError(http.StatusBadRequest, err)
			return
		}
		_ = resp.WriteAsJson(makeUserOut(in))
	}))

	ws.Route(ws.GET("/profile").To(func(req *restful.Request, resp *restful.Response) {
		_ = resp.WriteAsJson(profileFixture)
	}))

	mwRoute := ws.GET("/mw/ping").To(func(req *restful.Request, resp *restful.Response) {
		_, _ = resp.Write([]byte("pong"))
	})
	for i := 0; i < middlewareCount; i++ {
		mwRoute = mwRoute.Filter(func(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
			chain.ProcessFilter(req, resp)
		})
	}
	ws.Route(mwRoute)

	ws.Route(ws.PUT("/api/v1/users/{id}/orders").To(func(req *restful.Request, resp *restful.Response) {
		var in orderIn
		if err := req.ReadEntity(&in); err != nil {
			_ = resp.WriteError(http.StatusBadRequest, err)
			return
		}
		_ = resp.WriteAsJson(makeOrderOut(
			req.PathParameter("id"),
			req.QueryParameter("expand"),
			req.QueryParameter("currency"),
			req.HeaderParameter("X-Request-ID"),
			in,
		))
	}))

	idHandler := func(req *restful.Request, resp *restful.Response) {
		_, _ = resp.Write([]byte(req.PathParameter("id")))
	}
	okHandler := func(req *restful.Request, resp *restful.Response) {
		_, _ = resp.Write([]byte("ok"))
	}
	for _, rt := range scaleRoutes() {
		var rb *restful.RouteBuilder
		switch rt.method {
		case "GET":
			rb = ws.GET(rt.pattern)
		case "POST":
			rb = ws.POST(rt.pattern)
		case "PUT":
			rb = ws.PUT(rt.pattern)
		}
		if rt.hasID {
			ws.Route(rb.To(idHandler))
		} else {
			ws.Route(rb.To(okHandler))
		}
	}

	container.Add(ws)
	return container
}

func init() {
	register(&httpTarget{n: "go-restful", h: newGoRestful()}, nil)
}

package webbench

import (
	"net/http"

	"github.com/go-fuego/fuego"
)

// go-fuego/fuego — modern generics-based framework generating OpenAPI from
// code, built directly on the Go 1.22+ stdlib ServeMux. Architecturally the
// closest cousin to ghttp in this suite (typed handlers + net/http).

func newFuego() http.Handler {
	// Disable the default per-request logging middleware (slog + UUID request
	// IDs) so we measure the framework pipeline, not its logger.
	s := fuego.NewServer(
		fuego.WithoutLogger(),
		fuego.WithoutStartupMessages(),
		fuego.WithLoggingMiddleware(fuego.LoggingConfig{
			DisableRequest:  true,
			DisableResponse: true,
		}),
	)

	fuego.Get(s, "/ping", func(c fuego.ContextNoBody) (string, error) {
		return "pong", nil
	})

	fuego.Get(s, "/users/{id}", func(c fuego.ContextNoBody) (string, error) {
		return c.PathParam("id"), nil
	})

	fuego.Get(s, "/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}",
		func(c fuego.ContextNoBody) (string, error) {
			return c.PathParam("org") + c.PathParam("team") + c.PathParam("member") +
				c.PathParam("role") + c.PathParam("perm"), nil
		})

	fuego.Get(s, "/files/{path...}", func(c fuego.ContextNoBody) (string, error) {
		return c.PathParam("path"), nil
	})

	fuego.Get(s, "/search", func(c fuego.ContextNoBody) (string, error) {
		return c.QueryParam("q") + c.QueryParam("page") + c.QueryParam("limit"), nil
	})

	fuego.Post(s, "/users", func(c fuego.ContextWithBody[userIn]) (userOut, error) {
		in, err := c.Body()
		if err != nil {
			return userOut{}, err
		}
		return makeUserOut(in), nil
	})

	fuego.Get(s, "/profile", func(c fuego.ContextNoBody) (profile, error) {
		return profileFixture, nil
	})

	mw := fuego.Group(s, "/mw")
	for i := 0; i < middlewareCount; i++ {
		fuego.Use(mw, func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		})
	}
	fuego.Get(mw, "/ping", func(c fuego.ContextNoBody) (string, error) {
		return "pong", nil
	})

	fuego.Put(s, "/api/v1/users/{id}/orders", func(c fuego.ContextWithBody[orderIn]) (orderOut, error) {
		in, err := c.Body()
		if err != nil {
			return orderOut{}, err
		}
		return makeOrderOut(
			c.PathParam("id"),
			c.QueryParam("expand"),
			c.QueryParam("currency"),
			c.Request().Header.Get("X-Request-ID"),
			in,
		), nil
	})

	idHandler := func(c fuego.ContextNoBody) (string, error) {
		return c.PathParam("id"), nil
	}
	okHandler := func(c fuego.ContextNoBody) (string, error) {
		return "ok", nil
	}
	for _, rt := range scaleRoutes() {
		h := okHandler
		if rt.hasID {
			h = idHandler
		}
		switch rt.method {
		case "GET":
			fuego.Get(s, rt.pattern, h)
		case "POST":
			fuego.Post(s, rt.pattern, h)
		case "PUT":
			fuego.Put(s, rt.pattern, h)
		}
	}

	return s.Mux
}

func init() {
	register(&httpTarget{n: "fuego", h: newFuego()}, nil)
}

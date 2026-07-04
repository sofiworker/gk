package webbench

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// labstack/echo v4 — high performance, minimalist framework (net/http based).

func newEcho() http.Handler {
	e := echo.New()

	e.GET("/ping", func(c echo.Context) error {
		return c.String(http.StatusOK, "pong")
	})

	e.GET("/users/:id", func(c echo.Context) error {
		return c.String(http.StatusOK, c.Param("id"))
	})

	e.GET("/orgs/:org/teams/:team/members/:member/roles/:role/perms/:perm", func(c echo.Context) error {
		return c.String(http.StatusOK, c.Param("org")+c.Param("team")+c.Param("member")+
			c.Param("role")+c.Param("perm"))
	})

	e.GET("/files/*", func(c echo.Context) error {
		return c.String(http.StatusOK, c.Param("*"))
	})

	e.GET("/search", func(c echo.Context) error {
		return c.String(http.StatusOK, c.QueryParam("q")+c.QueryParam("page")+c.QueryParam("limit"))
	})

	e.POST("/users", func(c echo.Context) error {
		var in userIn
		if err := c.Bind(&in); err != nil {
			return c.String(http.StatusBadRequest, err.Error())
		}
		return c.JSON(http.StatusOK, makeUserOut(in))
	})

	e.GET("/profile", func(c echo.Context) error {
		return c.JSON(http.StatusOK, profileFixture)
	})

	mws := make([]echo.MiddlewareFunc, 0, middlewareCount)
	for i := 0; i < middlewareCount; i++ {
		mws = append(mws, func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { return next(c) }
		})
	}
	e.GET("/mw/ping", func(c echo.Context) error {
		return c.String(http.StatusOK, "pong")
	}, mws...)

	e.PUT("/api/v1/users/:id/orders", func(c echo.Context) error {
		var in orderIn
		if err := c.Bind(&in); err != nil {
			return c.String(http.StatusBadRequest, err.Error())
		}
		return c.JSON(http.StatusOK, makeOrderOut(
			c.Param("id"), c.QueryParam("expand"), c.QueryParam("currency"),
			c.Request().Header.Get("X-Request-ID"), in,
		))
	})

	idHandler := func(c echo.Context) error {
		return c.String(http.StatusOK, c.Param("id"))
	}
	okHandler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}
	for _, rt := range scaleRoutes() {
		if rt.hasID {
			pattern := rt.pattern[:len(rt.pattern)-len("{id}")] + ":id"
			e.Add(rt.method, pattern, idHandler)
		} else {
			e.Add(rt.method, rt.pattern, okHandler)
		}
	}

	return e
}

func init() {
	register(&httpTarget{n: "echo", h: newEcho()}, nil)
}

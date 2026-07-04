package webbench

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// gin-gonic/gin — the most widely used Go web framework (radix router,
// net/http based).

func newGin() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	r.GET("/users/:id", func(c *gin.Context) {
		c.String(http.StatusOK, c.Param("id"))
	})

	r.GET("/orgs/:org/teams/:team/members/:member/roles/:role/perms/:perm", func(c *gin.Context) {
		c.String(http.StatusOK, c.Param("org")+c.Param("team")+c.Param("member")+
			c.Param("role")+c.Param("perm"))
	})

	r.GET("/files/*path", func(c *gin.Context) {
		c.String(http.StatusOK, c.Param("path"))
	})

	r.GET("/search", func(c *gin.Context) {
		c.String(http.StatusOK, c.Query("q")+c.Query("page")+c.Query("limit"))
	})

	r.POST("/users", func(c *gin.Context) {
		var in userIn
		if err := c.ShouldBindJSON(&in); err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		c.JSON(http.StatusOK, makeUserOut(in))
	})

	r.GET("/profile", func(c *gin.Context) {
		c.JSON(http.StatusOK, profileFixture)
	})

	mws := make([]gin.HandlerFunc, 0, middlewareCount)
	for i := 0; i < middlewareCount; i++ {
		mws = append(mws, func(c *gin.Context) { c.Next() })
	}
	mw := r.Group("/mw", mws...)
	mw.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	r.PUT("/api/v1/users/:id/orders", func(c *gin.Context) {
		var in orderIn
		if err := c.ShouldBindJSON(&in); err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		c.JSON(http.StatusOK, makeOrderOut(
			c.Param("id"), c.Query("expand"), c.Query("currency"),
			c.GetHeader("X-Request-ID"), in,
		))
	})

	for _, rt := range scaleRoutes() {
		if rt.hasID {
			pattern := rt.pattern[:len(rt.pattern)-len("{id}")] + ":id"
			r.Handle(rt.method, pattern, func(c *gin.Context) {
				c.String(http.StatusOK, c.Param("id"))
			})
		} else {
			r.Handle(rt.method, rt.pattern, func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})
		}
	}

	return r
}

func init() {
	register(&httpTarget{n: "gin", h: newGin()}, nil)
}

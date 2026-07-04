package webbench

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/uptrace/bunrouter"
)

// uptrace/bunrouter — fast, modern net/http router with zero-alloc matching.

func bunPattern(p string) string {
	return strings.ReplaceAll(p, "{id}", ":id")
}

func newBunRouter() http.Handler {
	r := bunrouter.New()

	r.GET("/ping", func(w http.ResponseWriter, req bunrouter.Request) error {
		writeText(w, "pong")
		return nil
	})

	r.GET("/users/:id", func(w http.ResponseWriter, req bunrouter.Request) error {
		writeText(w, req.Param("id"))
		return nil
	})

	r.GET("/orgs/:org/teams/:team/members/:member/roles/:role/perms/:perm",
		func(w http.ResponseWriter, req bunrouter.Request) error {
			writeText(w, req.Param("org")+req.Param("team")+req.Param("member")+
				req.Param("role")+req.Param("perm"))
			return nil
		})

	r.GET("/files/*path", func(w http.ResponseWriter, req bunrouter.Request) error {
		writeText(w, req.Param("path"))
		return nil
	})

	r.GET("/search", func(w http.ResponseWriter, req bunrouter.Request) error {
		q := req.URL.Query()
		writeText(w, q.Get("q")+q.Get("page")+q.Get("limit"))
		return nil
	})

	r.POST("/users", func(w http.ResponseWriter, req bunrouter.Request) error {
		var in userIn
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil
		}
		writeJSON(w, makeUserOut(in))
		return nil
	})

	r.GET("/profile", func(w http.ResponseWriter, req bunrouter.Request) error {
		writeJSON(w, profileFixture)
		return nil
	})

	mws := make([]bunrouter.MiddlewareFunc, 0, middlewareCount)
	for i := 0; i < middlewareCount; i++ {
		mws = append(mws, func(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
			return func(w http.ResponseWriter, req bunrouter.Request) error {
				return next(w, req)
			}
		})
	}
	mwOpts := make([]bunrouter.GroupOption, 0, len(mws))
	for _, m := range mws {
		mwOpts = append(mwOpts, bunrouter.Use(m))
	}
	mw := r.NewGroup("/mw", mwOpts...)
	mw.GET("/ping", func(w http.ResponseWriter, req bunrouter.Request) error {
		writeText(w, "pong")
		return nil
	})

	r.PUT("/api/v1/users/:id/orders", func(w http.ResponseWriter, req bunrouter.Request) error {
		var in orderIn
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil
		}
		q := req.URL.Query()
		writeJSON(w, makeOrderOut(
			req.Param("id"), q.Get("expand"), q.Get("currency"),
			req.Header.Get("X-Request-ID"), in,
		))
		return nil
	})

	idHandler := func(w http.ResponseWriter, req bunrouter.Request) error {
		writeText(w, req.Param("id"))
		return nil
	}
	okHandler := func(w http.ResponseWriter, req bunrouter.Request) error {
		writeText(w, "ok")
		return nil
	}
	for _, rt := range scaleRoutes() {
		if rt.hasID {
			r.Handle(rt.method, bunPattern(rt.pattern), idHandler)
		} else {
			r.Handle(rt.method, rt.pattern, okHandler)
		}
	}

	return r
}

func init() {
	register(&httpTarget{n: "bunrouter", h: newBunRouter()}, nil)
}

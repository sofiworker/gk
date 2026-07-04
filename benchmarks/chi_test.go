package webbench

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// go-chi/chi v5 — lightweight, idiomatic net/http router.

func newChi() http.Handler {
	r := chi.NewRouter()

	r.Get("/ping", func(w http.ResponseWriter, req *http.Request) {
		writeText(w, "pong")
	})

	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		writeText(w, chi.URLParam(req, "id"))
	})

	r.Get("/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}",
		func(w http.ResponseWriter, req *http.Request) {
			writeText(w, chi.URLParam(req, "org")+chi.URLParam(req, "team")+
				chi.URLParam(req, "member")+chi.URLParam(req, "role")+chi.URLParam(req, "perm"))
		})

	r.Get("/files/*", func(w http.ResponseWriter, req *http.Request) {
		writeText(w, chi.URLParam(req, "*"))
	})

	r.Get("/search", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		writeText(w, q.Get("q")+q.Get("page")+q.Get("limit"))
	})

	r.Post("/users", func(w http.ResponseWriter, req *http.Request) {
		var in userIn
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, makeUserOut(in))
	})

	r.Get("/profile", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, profileFixture)
	})

	r.Route("/mw", func(r chi.Router) {
		for i := 0; i < middlewareCount; i++ {
			r.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					next.ServeHTTP(w, req)
				})
			})
		}
		r.Get("/ping", func(w http.ResponseWriter, req *http.Request) {
			writeText(w, "pong")
		})
	})

	r.Put("/api/v1/users/{id}/orders", func(w http.ResponseWriter, req *http.Request) {
		var in orderIn
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q := req.URL.Query()
		writeJSON(w, makeOrderOut(
			chi.URLParam(req, "id"), q.Get("expand"), q.Get("currency"),
			req.Header.Get("X-Request-ID"), in,
		))
	})

	idHandler := func(w http.ResponseWriter, req *http.Request) {
		writeText(w, chi.URLParam(req, "id"))
	}
	okHandler := func(w http.ResponseWriter, req *http.Request) {
		writeText(w, "ok")
	}
	for _, rt := range scaleRoutes() {
		if rt.hasID {
			r.MethodFunc(rt.method, rt.pattern, idHandler)
		} else {
			r.MethodFunc(rt.method, rt.pattern, okHandler)
		}
	}

	return r
}

func init() {
	register(&httpTarget{n: "chi", h: newChi()}, nil)
}

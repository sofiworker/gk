package webbench

import (
	"encoding/json"
	"net/http"
)

// net/http ServeMux (Go 1.22+ method/pattern routing) — the stdlib baseline.

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeText(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s))
}

func newStdMux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		writeText(w, "pong")
	})

	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeText(w, r.PathValue("id"))
	})

	mux.HandleFunc("GET /orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}",
		func(w http.ResponseWriter, r *http.Request) {
			writeText(w, r.PathValue("org")+r.PathValue("team")+r.PathValue("member")+
				r.PathValue("role")+r.PathValue("perm"))
		})

	mux.HandleFunc("GET /files/{path...}", func(w http.ResponseWriter, r *http.Request) {
		writeText(w, r.PathValue("path"))
	})

	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		writeText(w, q.Get("q")+q.Get("page")+q.Get("limit"))
	})

	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		var in userIn
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, makeUserOut(in))
	})

	mux.HandleFunc("GET /profile", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, profileFixture)
	})

	mux.Handle("GET /mw/ping", wrapPlainMiddlewares(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeText(w, "pong")
		}), middlewareCount))

	mux.HandleFunc("PUT /api/v1/users/{id}/orders", func(w http.ResponseWriter, r *http.Request) {
		var in orderIn
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q := r.URL.Query()
		writeJSON(w, makeOrderOut(
			r.PathValue("id"), q.Get("expand"), q.Get("currency"),
			r.Header.Get("X-Request-ID"), in,
		))
	})

	for _, rt := range scaleRoutes() {
		if rt.hasID {
			mux.HandleFunc(rt.method+" "+rt.pattern, func(w http.ResponseWriter, r *http.Request) {
				writeText(w, r.PathValue("id"))
			})
		} else {
			mux.HandleFunc(rt.method+" "+rt.pattern, func(w http.ResponseWriter, r *http.Request) {
				writeText(w, "ok")
			})
		}
	}

	return mux
}

func init() {
	register(&httpTarget{n: "stdmux", h: newStdMux()}, nil)
}

package webbench

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// gorilla/mux — the classic, regexp-capable net/http router.

func newGorilla() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/ping", func(w http.ResponseWriter, req *http.Request) {
		writeText(w, "pong")
	}).Methods(http.MethodGet)

	r.HandleFunc("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		writeText(w, mux.Vars(req)["id"])
	}).Methods(http.MethodGet)

	r.HandleFunc("/orgs/{org}/teams/{team}/members/{member}/roles/{role}/perms/{perm}",
		func(w http.ResponseWriter, req *http.Request) {
			v := mux.Vars(req)
			writeText(w, v["org"]+v["team"]+v["member"]+v["role"]+v["perm"])
		}).Methods(http.MethodGet)

	r.HandleFunc("/files/{path:.*}", func(w http.ResponseWriter, req *http.Request) {
		writeText(w, mux.Vars(req)["path"])
	}).Methods(http.MethodGet)

	r.HandleFunc("/search", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		writeText(w, q.Get("q")+q.Get("page")+q.Get("limit"))
	}).Methods(http.MethodGet)

	r.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		var in userIn
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, makeUserOut(in))
	}).Methods(http.MethodPost)

	r.HandleFunc("/profile", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, profileFixture)
	}).Methods(http.MethodGet)

	sub := r.PathPrefix("/mw").Subrouter()
	for i := 0; i < middlewareCount; i++ {
		sub.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req)
			})
		})
	}
	sub.HandleFunc("/ping", func(w http.ResponseWriter, req *http.Request) {
		writeText(w, "pong")
	}).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/users/{id}/orders", func(w http.ResponseWriter, req *http.Request) {
		var in orderIn
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q := req.URL.Query()
		writeJSON(w, makeOrderOut(
			mux.Vars(req)["id"], q.Get("expand"), q.Get("currency"),
			req.Header.Get("X-Request-ID"), in,
		))
	}).Methods(http.MethodPut)

	idHandler := func(w http.ResponseWriter, req *http.Request) {
		writeText(w, mux.Vars(req)["id"])
	}
	okHandler := func(w http.ResponseWriter, req *http.Request) {
		writeText(w, "ok")
	}
	for _, rt := range scaleRoutes() {
		if rt.hasID {
			r.HandleFunc(rt.pattern, idHandler).Methods(rt.method)
		} else {
			r.HandleFunc(rt.pattern, okHandler).Methods(rt.method)
		}
	}

	return r
}

func init() {
	register(&httpTarget{n: "gorilla-mux", h: newGorilla()}, nil)
}

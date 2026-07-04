package webbench

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
)

// julienschmidt/httprouter — the minimal radix-tree router that many
// frameworks (gin included) descend from. No middleware concept: the
// middleware scenario wraps the final handle manually.

func hrPattern(p string) string {
	p = strings.ReplaceAll(p, "{id}", ":id")
	return p
}

func newHTTPRouter() http.Handler {
	r := httprouter.New()

	r.GET("/ping", func(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
		writeText(w, "pong")
	})

	r.GET("/users/:id", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		writeText(w, ps.ByName("id"))
	})

	r.GET("/orgs/:org/teams/:team/members/:member/roles/:role/perms/:perm",
		func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
			writeText(w, ps.ByName("org")+ps.ByName("team")+ps.ByName("member")+
				ps.ByName("role")+ps.ByName("perm"))
		})

	r.GET("/files/*filepath", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		writeText(w, ps.ByName("filepath"))
	})

	r.GET("/search", func(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
		q := req.URL.Query()
		writeText(w, q.Get("q")+q.Get("page")+q.Get("limit"))
	})

	r.POST("/users", func(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
		var in userIn
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, makeUserOut(in))
	})

	r.GET("/profile", func(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
		writeJSON(w, profileFixture)
	})

	mwHandle := func(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
		writeText(w, "pong")
	}
	for i := 0; i < middlewareCount; i++ {
		next := mwHandle
		mwHandle = func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
			next(w, req, ps)
		}
	}
	r.GET("/mw/ping", mwHandle)

	r.PUT("/api/v1/users/:id/orders", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		var in orderIn
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q := req.URL.Query()
		writeJSON(w, makeOrderOut(
			ps.ByName("id"), q.Get("expand"), q.Get("currency"),
			req.Header.Get("X-Request-ID"), in,
		))
	})

	idHandle := func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		writeText(w, ps.ByName("id"))
	}
	okHandle := func(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
		writeText(w, "ok")
	}
	for _, rt := range scaleRoutes() {
		if rt.hasID {
			r.Handle(rt.method, hrPattern(rt.pattern), idHandle)
		} else {
			r.Handle(rt.method, rt.pattern, okHandle)
		}
	}

	return r
}

func init() {
	register(&httpTarget{n: "httprouter", h: newHTTPRouter()}, nil)
}

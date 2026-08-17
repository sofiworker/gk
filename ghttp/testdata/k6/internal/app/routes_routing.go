package app

import (
	"encoding/json"
	"net/http"

	"github.com/sofiworker/gk/ghttp"
)

func registerRouting(server *ghttp.Server, state *stateController, metrics *RuntimeMetrics, faults *FaultController) {
	rawJSON := func(value any) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, value)
		})
	}

	server.MustMount(ghttp.RawOperation(http.MethodGet, "/health", rawJSON(map[string]string{"status": "ok"})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/ready", rawJSON(map[string]string{"status": "ready"})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/routes/static", rawJSON(map[string]string{"route": "static"})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/routes/users/new", rawJSON(map[string]string{"id": "new", "route": "static"})))

	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/users/{id}"), ghttp.StructInput[ghttp.Params](), jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"id": params.Path("id")}
	})))
	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/pairs/{left}/{right}"), ghttp.StructInput[ghttp.Params](), jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"left": params.Path("left"), "right": params.Path("right")}
	})))
	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/files/{path...}"), ghttp.StructInput[ghttp.Params](), jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"path": params.Path("path")}
	})))
	server.Group("/routes/groups/v1").MustMount(ghttp.HandleHTTP(ghttp.Get("/items/{id}"), ghttp.StructInput[ghttp.Params](), jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"id": params.Path("id")}
	})))
	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/unicode/{value}"), ghttp.StructInput[ghttp.Params](), jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"value": params.Path("value")}
	})))

	methodHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	server.MustMount(ghttp.RawOperation(http.MethodPost, "/routes/method", methodHandler))
	server.MustMount(ghttp.RawOperation(http.MethodDelete, "/routes/method", methodHandler))

	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/raw-path/{value}"), ghttp.StructInput[ghttp.Params](), jsonParams(func(r *http.Request, params ghttp.Params) any {
		return map[string]string{
			"value":        params.Path("value"),
			"path_value":   r.PathValue("value"),
			"path":         r.URL.Path,
			"raw_path":     r.URL.RawPath,
			"escaped_path": r.URL.EscapedPath(),
			"raw_query":    r.URL.RawQuery,
		}
	})))

	server.MustMount(ghttp.RawOperation(http.MethodGet, "/__test/metrics", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, metrics.Snapshot())
	})))
	server.MustMount(ghttp.RawOperation(http.MethodPost, "/__test/reset", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		state.reset()
		metrics.Reset()
		faults.Reset()
		w.WriteHeader(http.StatusNoContent)
	})))
}

func jsonParams(value func(*http.Request, ghttp.Params) any) ghttp.HTTPHandlerFunc[ghttp.Params] {
	return func(w http.ResponseWriter, r *http.Request, params ghttp.Params) error {
		writeJSON(w, value(r, params))
		return nil
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

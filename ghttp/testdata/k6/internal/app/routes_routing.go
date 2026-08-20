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

	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/users/{id}"), ghttp.PathString("id"), jsonValue(func(_ *http.Request, id string) any {
		return map[string]string{"id": id}
	})))
	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/pairs/{left}/{right}"),
		ghttp.MapInputs(ghttp.PathString("left"), ghttp.PathString("right"), func(left, right string) struct{ Left, Right string } {
			return struct{ Left, Right string }{Left: left, Right: right}
		}),
		jsonValue(func(_ *http.Request, in struct{ Left, Right string }) any {
			return map[string]string{"left": in.Left, "right": in.Right}
		})))
	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/files/{path...}"), ghttp.PathString("path"), jsonValue(func(_ *http.Request, path string) any {
		return map[string]string{"path": path}
	})))
	server.Group("/routes/groups/v1").MustMount(ghttp.HandleHTTP(ghttp.Get("/items/{id}"), ghttp.PathString("id"), jsonValue(func(_ *http.Request, id string) any {
		return map[string]string{"id": id}
	})))
	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/unicode/{value}"), ghttp.PathString("value"), jsonValue(func(_ *http.Request, value string) any {
		return map[string]string{"value": value}
	})))

	methodHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	server.MustMount(ghttp.RawOperation(http.MethodPost, "/routes/method", methodHandler))
	server.MustMount(ghttp.RawOperation(http.MethodDelete, "/routes/method", methodHandler))

	server.MustMount(ghttp.HandleHTTP(ghttp.Get("/routes/raw-path/{value}"), ghttp.PathString("value"), jsonValue(func(r *http.Request, value string) any {
		return map[string]string{
			"value":        value,
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

func jsonValue[I any](value func(*http.Request, I) any) ghttp.HTTPHandlerFunc[I] {
	return func(w http.ResponseWriter, r *http.Request, input I) error {
		writeJSON(w, value(r, input))
		return nil
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

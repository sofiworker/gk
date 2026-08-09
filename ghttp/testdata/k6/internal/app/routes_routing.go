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

	ghttp.Route[struct{}, struct{}](server).GET("/health").ToHTTP(rawJSON(map[string]string{"status": "ok"}))
	ghttp.Route[struct{}, struct{}](server).GET("/ready").ToHTTP(rawJSON(map[string]string{"status": "ready"}))
	ghttp.Route[struct{}, struct{}](server).GET("/routes/static").ToHTTP(rawJSON(map[string]string{"route": "static"}))
	ghttp.Route[struct{}, struct{}](server).GET("/routes/users/new").ToHTTP(rawJSON(map[string]string{"id": "new", "route": "static"}))

	ghttp.Route[ghttp.Params, struct{}](server).GET("/routes/users/{id}").ToHTTPFunc(jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"id": params.Path("id")}
	}))
	ghttp.Route[ghttp.Params, struct{}](server).GET("/routes/pairs/{left}/{right}").ToHTTPFunc(jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"left": params.Path("left"), "right": params.Path("right")}
	}))
	ghttp.Route[ghttp.Params, struct{}](server).GET("/routes/files/{path...}").ToHTTPFunc(jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"path": params.Path("path")}
	}))
	ghttp.Route[ghttp.Params, struct{}](server.Group("/routes/groups/v1")).GET("/items/{id}").ToHTTPFunc(jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"id": params.Path("id")}
	}))
	ghttp.Route[ghttp.Params, struct{}](server).GET("/routes/unicode/{value}").ToHTTPFunc(jsonParams(func(_ *http.Request, params ghttp.Params) any {
		return map[string]string{"value": params.Path("value")}
	}))

	methodHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	ghttp.Route[struct{}, struct{}](server).POST("/routes/method").ToHTTP(methodHandler)
	ghttp.Route[struct{}, struct{}](server).DELETE("/routes/method").ToHTTP(methodHandler)

	ghttp.Route[ghttp.Params, struct{}](server).GET("/routes/raw-path/{value}").ToHTTPFunc(jsonParams(func(r *http.Request, params ghttp.Params) any {
		return map[string]string{
			"value":        params.Path("value"),
			"path_value":   r.PathValue("value"),
			"path":         r.URL.Path,
			"raw_path":     r.URL.RawPath,
			"escaped_path": r.URL.EscapedPath(),
			"raw_query":    r.URL.RawQuery,
		}
	}))

	ghttp.Route[struct{}, struct{}](server).GET("/__test/metrics").ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, metrics.Snapshot())
	}))
	ghttp.Route[struct{}, struct{}](server).POST("/__test/reset").ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		state.reset()
		metrics.Reset()
		faults.Reset()
		w.WriteHeader(http.StatusNoContent)
	}))
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

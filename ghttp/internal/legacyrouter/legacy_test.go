package legacyrouter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyRoutersMatchRouteClassesAndMethodOutcomes(t *testing.T) {
	for _, factory := range legacyRouterFactories() {
		t.Run(factory.name, func(t *testing.T) {
			router := factory.new()
			registerLegacyOutcomeRoutes(t, router)

			for _, request := range []struct {
				name       string
				method     string
				path       string
				status     int
				headerName string
				header     string
				body       string
				checkBody  bool
				allow      string
			}{
				{name: "static", method: http.MethodGet, path: "/users/new", status: http.StatusOK, headerName: "X-Route", header: "static", body: "static", checkBody: true},
				{name: "parameter", method: http.MethodGet, path: "/users/42", status: http.StatusOK, headerName: "X-Route", header: "parameter", body: "parameter", checkBody: true},
				{name: "catch-all", method: http.MethodGet, path: "/files/css/app.css", status: http.StatusOK, headerName: "X-Route", header: "catch-all", body: "catch-all", checkBody: true},
				{name: "explicit-head", method: http.MethodHead, path: "/users/new", status: http.StatusOK, headerName: "X-Route", header: "explicit-head", checkBody: true},
				{name: "get-head-fallback", method: http.MethodHead, path: "/users/42", status: http.StatusOK, headerName: "X-Route", header: "parameter"},
				{name: "method-not-allowed", method: http.MethodPost, path: "/users/42", status: http.StatusMethodNotAllowed, allow: "GET, HEAD, PUT"},
				{name: "not-found", method: http.MethodGet, path: "/missing", status: http.StatusNotFound},
			} {
				t.Run(request.name, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, httptest.NewRequest(request.method, request.path, nil))
					if recorder.Code != request.status {
						t.Fatalf("status = %d, want %d", recorder.Code, request.status)
					}
					if got := recorder.Header().Get(request.headerName); got != request.header {
						t.Fatalf("%s = %q, want %q", request.headerName, got, request.header)
					}
					if request.checkBody && recorder.Body.String() != request.body {
						got := recorder.Body.String()
						t.Fatalf("body = %q, want %q", got, request.body)
					}
					if got := recorder.Header().Get("Allow"); got != request.allow {
						t.Fatalf("Allow = %q, want %q", got, request.allow)
					}
				})
			}
		})
	}
}

func TestLegacyRoutersDeliverPathParamsToAdapter(t *testing.T) {
	for _, factory := range legacyRouterFactories() {
		t.Run(factory.name, func(t *testing.T) {
			router := factory.new()
			handler := &pathParamRecordingHandler{}
			if err := router.RegisterWithPathParams(http.MethodGet, "/orgs/{orgID}/users/{userID}/files/{path...}", handler); err != nil {
				t.Fatalf("RegisterWithPathParams() error = %v", err)
			}

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/orgs/acme/users/alice/files/css/app.css", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			for key, want := range map[string]string{"orgID": "acme", "userID": "alice", "path": "css/app.css"} {
				if got := handler.params.Get(key); got != want {
					t.Fatalf("path parameter %s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

type legacyRouterFactory struct {
	name string
	new  func() Router
}

func legacyRouterFactories() []legacyRouterFactory {
	return []legacyRouterFactory{
		{name: "radix", new: func() Router { return NewRadix() }},
		{name: "compiled", new: func() Router { return NewCompiled() }},
		{name: "matchit", new: func() Router { return NewMatchit() }},
		{name: "std", new: func() Router { return NewStd() }},
	}
}

func registerLegacyOutcomeRoutes(t *testing.T, router Router) {
	t.Helper()
	for _, route := range []struct {
		method string
		path   string
		name   string
		status int
		body   string
	}{
		{method: http.MethodGet, path: "/users/new", name: "static", body: "static"},
		{method: http.MethodGet, path: "/users/{id}", name: "parameter", body: "parameter"},
		{method: http.MethodGet, path: "/files/{path...}", name: "catch-all", body: "catch-all"},
		{method: http.MethodHead, path: "/users/new", name: "explicit-head"},
		{method: http.MethodPut, path: "/users/{id}", name: "put"},
	} {
		route := route
		if err := router.Register(route.method, route.path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Route", route.name)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(route.body))
		})); err != nil {
			t.Fatalf("Register(%s %s) error = %v", route.method, route.path, err)
		}
	}
}

type pathParamRecordingHandler struct {
	params PathParams
}

func (h *pathParamRecordingHandler) ServeHTTPWithPathParams(w http.ResponseWriter, _ *http.Request, params PathParams) {
	h.params = params
	w.WriteHeader(http.StatusOK)
}

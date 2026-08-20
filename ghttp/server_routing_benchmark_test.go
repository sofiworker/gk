package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sofiworker/gk/ghttp/internal/legacyrouter"
)

type benchmarkResponseWriter struct {
	header http.Header
	status int
}

func newBenchmarkResponseWriter() *benchmarkResponseWriter {
	return &benchmarkResponseWriter{header: make(http.Header), status: http.StatusOK}
}

func (w *benchmarkResponseWriter) Header() http.Header          { return w.header }
func (*benchmarkResponseWriter) Write(data []byte) (int, error) { return len(data), nil }
func (w *benchmarkResponseWriter) WriteHeader(status int)       { w.status = status }

type benchmarkRequestHarness struct {
	handler http.Handler
	request *http.Request
	writer  *benchmarkResponseWriter
}

type benchmarkRequestAdapter struct {
	handler http.Handler
}

func (a benchmarkRequestAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

type benchmarkLegacyRequestAdapter struct {
	router legacyrouter.Router
}

func (a benchmarkLegacyRequestAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &Ctx{W: w, R: r}))
	a.router.ServeHTTP(w, r)
}

func (h benchmarkRequestHarness) ServeHTTP() {
	h.handler.ServeHTTP(h.writer, h.request)
}

func BenchmarkRouteComparison(b *testing.B) {
	implementation := os.Getenv("GHTTP_ROUTER_IMPL")
	if implementation == "" {
		implementation = "new"
	}
	if !isBenchmarkRouterImplementation(implementation) {
		b.Fatalf("GHTTP_ROUTER_IMPL = %q, want new, legacy, legacy-radix, legacy-compiled, legacy-matchit, or legacy-std", implementation)
	}

	for _, routeCount := range []int{16, 128, 1024, 8192} {
		for _, scenario := range benchmarkScenarios(routeCount) {
			b.Run(fmt.Sprintf("routes=%d/%s", routeCount, scenario.name), func(b *testing.B) {
				harness := newBenchmarkRequestHarness(implementation, routeCount, scenario)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					harness.ServeHTTP()
				}
			})
		}
	}
}

func TestBenchmarkRequestHarnessesServeFixedScenarios(t *testing.T) {
	statusByScenario := map[string]int{
		"static":            http.StatusOK,
		"param":             http.StatusOK,
		"deep-param":        http.StatusOK,
		"catch-all":         http.StatusOK,
		"head-explicit":     http.StatusOK,
		"head-get-fallback": http.StatusOK,
		"method-fallback":   http.StatusMethodNotAllowed,
		"typed-extraction":  http.StatusOK,
		"404":               http.StatusNotFound,
		"405":               http.StatusMethodNotAllowed,
	}

	for _, implementation := range []string{"new", "legacy", "legacy-radix", "legacy-compiled", "legacy-matchit", "legacy-std"} {
		for _, routeCount := range []int{16, 128, 1024, 8192} {
			for _, scenario := range benchmarkScenarios(routeCount) {
				t.Run(fmt.Sprintf("%s/routes=%d/%s", implementation, routeCount, scenario.name), func(t *testing.T) {
					harness := newBenchmarkRequestHarness(implementation, routeCount, scenario)
					harness.ServeHTTP()
					if got, want := harness.writer.status, statusByScenario[scenario.name]; got != want {
						t.Fatalf("status = %d, want %d", got, want)
					}
				})
			}
		}
	}
}

func TestBenchmarkLegacyAdapterSelectsFrozenRouter(t *testing.T) {
	for _, test := range []struct {
		implementation string
		want           any
	}{
		{implementation: "legacy", want: (*legacyrouter.Radix)(nil)},
		{implementation: "legacy-radix", want: (*legacyrouter.Radix)(nil)},
		{implementation: "legacy-compiled", want: (*legacyrouter.Compiled)(nil)},
		{implementation: "legacy-matchit", want: (*legacyrouter.Matchit)(nil)},
		{implementation: "legacy-std", want: (*legacyrouter.Std)(nil)},
	} {
		t.Run(test.implementation, func(t *testing.T) {
			adapter, ok := newBenchmarkRequestAdapter(test.implementation, 16).(benchmarkRequestAdapter)
			if !ok {
				t.Fatalf("handler type = %T, want benchmarkRequestAdapter", newBenchmarkRequestAdapter(test.implementation, 16))
			}
			legacy, ok := adapter.handler.(benchmarkLegacyRequestAdapter)
			if !ok {
				t.Fatalf("inner handler type = %T, want benchmarkLegacyRequestAdapter", adapter.handler)
			}
			if got, want := fmt.Sprintf("%T", legacy.router), fmt.Sprintf("%T", test.want); got != want {
				t.Fatalf("router type = %s, want %s", got, want)
			}
		})
	}
}

func BenchmarkRouteFreeze(b *testing.B) {
	for _, routeCount := range []int{16, 128, 1024, 8192} {
		for _, middlewareCount := range []int{0, 2, 8} {
			b.Run(fmt.Sprintf("routes=%d/middleware=%d", routeCount, middlewareCount), func(b *testing.B) {
				chains := 0
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					server := New()
					for middleware := 0; middleware < middlewareCount; middleware++ {
						server.Use(func(c *Ctx) { c.Next() })
					}
					registerBenchmarkServerRoutes(server, routeCount)
					b.StartTimer()
					server.finalizeRoutes()
					chains = benchmarkCompiledChainCount(server.compiled.Load())
				}
				b.ReportMetric(float64(chains), "chains")
			})
		}
	}
}

type benchmarkScenario struct {
	name   string
	method string
	path   string
}

func benchmarkScenarios(routeCount int) []benchmarkScenario {
	static := benchmarkRouteIndex(routeCount, benchmarkRouteStatic)
	param := benchmarkRouteIndex(routeCount, benchmarkRouteParam)
	deepParam := benchmarkRouteIndex(routeCount, benchmarkRouteDeepParam)
	catchAll := benchmarkRouteIndex(routeCount, benchmarkRouteCatchAll)
	typed := benchmarkRouteIndex(routeCount, benchmarkRouteTyped)
	headExplicit := benchmarkRouteIndex(routeCount, benchmarkRouteHeadExplicit)
	return []benchmarkScenario{
		{name: "static", method: http.MethodGet, path: fmt.Sprintf("/static/%04d", static)},
		{name: "param", method: http.MethodGet, path: fmt.Sprintf("/users/%04d/42", param)},
		{name: "deep-param", method: http.MethodGet, path: fmt.Sprintf("/api/%04d/orgs/acme/users/alice/posts/99", deepParam)},
		{name: "catch-all", method: http.MethodGet, path: fmt.Sprintf("/assets/%04d/css/app.css", catchAll)},
		{name: "head-explicit", method: http.MethodHead, path: fmt.Sprintf("/head/%04d", headExplicit)},
		{name: "head-get-fallback", method: http.MethodHead, path: fmt.Sprintf("/static/%04d", static)},
		{name: "method-fallback", method: http.MethodPost, path: fmt.Sprintf("/static/%04d", static)},
		{name: "typed-extraction", method: http.MethodGet, path: fmt.Sprintf("/typed/%04d/42", typed)},
		{name: "404", method: http.MethodGet, path: "/missing/not-found"},
		{name: "405", method: http.MethodPost, path: fmt.Sprintf("/users/%04d/42", param)},
	}
}

const (
	benchmarkRouteStatic = iota
	benchmarkRouteParam
	benchmarkRouteDeepParam
	benchmarkRouteCatchAll
	benchmarkRouteTyped
	benchmarkRouteHeadExplicit
	benchmarkRouteKinds
)

func benchmarkRouteIndex(routeCount, kind int) int {
	for index := routeCount / 2; index < routeCount; index++ {
		if index%benchmarkRouteKinds == kind {
			return index
		}
	}
	for index := routeCount/2 - 1; index >= 0; index-- {
		if index%benchmarkRouteKinds == kind {
			return index
		}
	}
	return kind
}

func newBenchmarkRequestHarness(implementation string, routeCount int, scenario benchmarkScenario) benchmarkRequestHarness {
	return benchmarkRequestHarness{
		handler: newBenchmarkRequestAdapter(implementation, routeCount),
		request: httptest.NewRequest(scenario.method, scenario.path, nil),
		writer:  newBenchmarkResponseWriter(),
	}
}

func newBenchmarkRequestAdapter(implementation string, routeCount int) http.Handler {
	if router := newBenchmarkLegacyRouter(implementation, routeCount); router != nil {
		return benchmarkRequestAdapter{handler: benchmarkLegacyRequestAdapter{router: router}}
	}
	server := newBenchmarkServer(routeCount)
	return benchmarkRequestAdapter{handler: server}
}

func isBenchmarkRouterImplementation(implementation string) bool {
	switch implementation {
	case "new", "legacy", "legacy-radix", "legacy-compiled", "legacy-matchit", "legacy-std":
		return true
	default:
		return false
	}
}

func benchmarkCompiledChainCount(state *compiledState) int {
	if state == nil || state.mux == nil {
		return 0
	}
	// 冻结状态拥有一套编译路由链与三套结果链；a frozen state owns compiled route and outcome chains.
	return len(state.mux.routes) + 3
}

func newBenchmarkServer(routeCount int) *Server {
	server := New()
	registerBenchmarkServerRoutes(server, routeCount)
	server.finalizeRoutes()
	return server
}

func registerBenchmarkServerRoutes(server *Server, routeCount int) {
	for index := 0; index < routeCount; index++ {
		method := http.MethodGet
		if index%benchmarkRouteKinds == benchmarkRouteHeadExplicit {
			method = http.MethodHead
		}
		if index%benchmarkRouteKinds == benchmarkRouteTyped {
			server.MustMount(HandleHTTP(Endpoint(method, benchmarkRoutePattern(index)), PathString("id"), benchmarkTypedHandler))
			continue
		}
		server.MustMount(RawOperation(method, benchmarkRoutePattern(index), benchmarkRawHandler))
	}
}

func newBenchmarkLegacyRouter(implementation string, routeCount int) legacyrouter.Router {
	var router legacyrouter.Router
	switch implementation {
	case "legacy", "legacy-radix":
		router = legacyrouter.NewRadix()
	case "legacy-compiled":
		router = legacyrouter.NewCompiled()
	case "legacy-matchit":
		router = legacyrouter.NewMatchit()
	case "legacy-std":
		router = legacyrouter.NewStd()
	default:
		return nil
	}
	for index := 0; index < routeCount; index++ {
		method := http.MethodGet
		if index%benchmarkRouteKinds == benchmarkRouteHeadExplicit {
			method = http.MethodHead
		}
		if index%benchmarkRouteKinds == benchmarkRouteTyped {
			if err := router.RegisterWithPathParams(method, benchmarkRoutePattern(index), benchmarkLegacyTypedHandler{}); err != nil {
				panic(err)
			}
			continue
		}
		if err := router.Register(method, benchmarkRoutePattern(index), benchmarkRawHandler); err != nil {
			panic(err)
		}
	}
	return router
}

var benchmarkRawHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

func benchmarkTypedHandler(_ http.ResponseWriter, _ *http.Request, id string) error {
	_ = id
	return nil
}

type benchmarkLegacyTypedHandler struct{}

func (benchmarkLegacyTypedHandler) ServeHTTPWithPathParams(_ http.ResponseWriter, r *http.Request, params legacyrouter.PathParams) {
	var path pathParamList
	params.Range(func(param legacyrouter.PathParam) {
		path.Add(param.Key, param.Value)
	})
	input := new(Params)
	*input = paramsFromRequestWithPathParams(r, nil, path)
	_ = input.Path("id")
}

func benchmarkRoutePattern(index int) string {
	switch index % benchmarkRouteKinds {
	case benchmarkRouteStatic:
		return fmt.Sprintf("/static/%04d", index)
	case benchmarkRouteParam:
		return fmt.Sprintf("/users/%04d/{id}", index)
	case benchmarkRouteDeepParam:
		return fmt.Sprintf("/api/%04d/orgs/{orgID}/users/{userID}/posts/{postID}", index)
	case benchmarkRouteCatchAll:
		return fmt.Sprintf("/assets/%04d/{path...}", index)
	case benchmarkRouteTyped:
		return fmt.Sprintf("/typed/%04d/{id}", index)
	default:
		return fmt.Sprintf("/head/%04d", index)
	}
}

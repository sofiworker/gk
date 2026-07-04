package webbench

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
)

// -----------------------------------------------------------------------------
// Scenarios
//
// Every framework registers the same route table and handlers with identical
// semantics. Benchmarks are grouped by scenario so that
// `go test -bench BenchmarkStaticRoute` prints one line per framework and
// results can be compared directly (or via benchstat).
// -----------------------------------------------------------------------------

type scenario struct {
	key        string
	method     string
	targets    []string // requests cycle through these
	body       []byte
	headers    map[string]string
	wantStatus int
}

var (
	userJSON  = []byte(`{"name":"Alice","email":"alice@example.com","age":30,"active":true,"tags":["go","web","bench"]}`)
	orderJSON = []byte(`{"items":[{"sku":"A-1","qty":2,"price":9.99},{"sku":"B-2","qty":1,"price":199.0},{"sku":"C-3","qty":5,"price":2.5}],"note":"deliver asap","coupon":"WELCOME10"}`)

	scStatic = scenario{
		key: "static", method: "GET",
		targets: []string{"/ping"}, wantStatus: 200,
	}
	scParam1 = scenario{
		key: "param1", method: "GET",
		targets: []string{"/users/12345"}, wantStatus: 200,
	}
	scParam5 = scenario{
		key: "param5", method: "GET",
		targets:    []string{"/orgs/o-1/teams/t-2/members/m-3/roles/r-4/perms/p-5"},
		wantStatus: 200,
	}
	scWildcard = scenario{
		key: "wildcard", method: "GET",
		targets: []string{"/files/assets/css/site.css"}, wantStatus: 200,
	}
	scQuery = scenario{
		key: "query", method: "GET",
		targets: []string{"/search?q=golang&page=2&limit=50"}, wantStatus: 200,
	}
	scJSONBind = scenario{
		key: "json_bind", method: "POST",
		targets: []string{"/users"}, body: userJSON, wantStatus: 200,
	}
	scJSONResp = scenario{
		key: "json_resp", method: "GET",
		targets: []string{"/profile"}, wantStatus: 200,
	}
	scMiddleware = scenario{
		key: "middleware5", method: "GET",
		targets: []string{"/mw/ping"}, wantStatus: 200,
	}
	scFullChain = scenario{
		key: "full_chain", method: "PUT",
		targets: []string{"/api/v1/users/12345/orders?expand=items&currency=CNY"},
		body:    orderJSON,
		headers: map[string]string{
			"X-Request-ID":  "req-abc-123",
			"Authorization": "Bearer benchtoken",
		},
		wantStatus: 200,
	}
	scScale = scenario{
		key: "route_scale_200", method: "GET",
		targets: []string{
			"/api/v1/res0",
			"/api/v1/res0/1001",
			"/api/v1/res12/77",
			"/api/v1/res25",
			"/api/v1/res37/12345",
			"/api/v1/res49/9",
		},
		wantStatus: 200,
	}
	scNotFound = scenario{
		key: "not_found", method: "GET",
		targets: []string{"/definitely/not/registered/xyz"}, wantStatus: 404,
	}

	allScenarios = []scenario{
		scStatic, scParam1, scParam5, scWildcard, scQuery,
		scJSONBind, scJSONResp, scMiddleware, scFullChain,
		scScale, scNotFound,
	}
)

// scaleTargetPaths returns the full 200-route table for the route-scale
// scenario: 50 resources x (GET list, GET item, POST list, PUT item).
type scaleRoute struct {
	method  string
	pattern string // uses {id} placeholder; adapters translate syntax
	hasID   bool
}

func scaleRoutes() []scaleRoute {
	routes := make([]scaleRoute, 0, scaleResourceCount*4)
	for i := 0; i < scaleResourceCount; i++ {
		base := fmt.Sprintf("/api/v1/res%d", i)
		routes = append(routes,
			scaleRoute{"GET", base, false},
			scaleRoute{"GET", base + "/{id}", true},
			scaleRoute{"POST", base, false},
			scaleRoute{"PUT", base + "/{id}", true},
		)
	}
	return routes
}

// -----------------------------------------------------------------------------
// Adapter registry
// -----------------------------------------------------------------------------

// target is one framework under benchmark.
type target interface {
	name() string
	bench(b *testing.B, sc scenario)
	benchParallel(b *testing.B, sc scenario)
	// probe issues one request (first target) and returns status + body,
	// used by the sanity test to prove all frameworks do equivalent work.
	probe(sc scenario) (int, string, error)
}

type registryEntry struct {
	t     target
	skips map[string]string // scenario key -> reason
}

var (
	registryMu sync.Mutex
	registry   []registryEntry
)

func register(t target, skips map[string]string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = append(registry, registryEntry{t: t, skips: skips})
}

func sortedRegistry() []registryEntry {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]registryEntry, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].t.name() < out[j].t.name() })
	return out
}

// -----------------------------------------------------------------------------
// net/http based adapter (used by every framework except fiber and hertz)
// -----------------------------------------------------------------------------

// mockWriter is a reusable, discarding http.ResponseWriter, mirroring the
// approach of julienschmidt/go-http-routing-benchmark so recorder allocations
// do not drown out framework allocations.
type mockWriter struct {
	headers http.Header
	status  int
	written int
}

func newMockWriter() *mockWriter { return &mockWriter{headers: make(http.Header)} }

func (m *mockWriter) Header() http.Header { return m.headers }

func (m *mockWriter) Write(p []byte) (int, error) {
	m.written += len(p)
	return len(p), nil
}

func (m *mockWriter) WriteString(s string) (int, error) {
	m.written += len(s)
	return len(s), nil
}

func (m *mockWriter) WriteHeader(code int) { m.status = code }

func (m *mockWriter) Flush() {}

type httpTarget struct {
	n string
	h http.Handler
}

func (t *httpTarget) name() string { return t.n }

func buildRequests(sc scenario) []*http.Request {
	reqs := make([]*http.Request, 0, len(sc.targets))
	for _, tgt := range sc.targets {
		req := httptest.NewRequest(sc.method, tgt, nil)
		for k, v := range sc.headers {
			req.Header.Set(k, v)
		}
		if len(sc.body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		reqs = append(reqs, req)
	}
	return reqs
}

func (t *httpTarget) bench(b *testing.B, sc scenario) {
	reqs := buildRequests(sc)
	w := newMockWriter()
	var br *bytes.Reader
	var rc io.ReadCloser
	if len(sc.body) > 0 {
		br = bytes.NewReader(sc.body)
		rc = io.NopCloser(br)
	}
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		req := reqs[i%len(reqs)]
		if br != nil {
			br.Reset(sc.body)
			req.Body = rc
			req.ContentLength = int64(len(sc.body))
		}
		t.h.ServeHTTP(w, req)
		i++
	}
}

func (t *httpTarget) benchParallel(b *testing.B, sc scenario) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		reqs := buildRequests(sc)
		w := newMockWriter()
		var br *bytes.Reader
		var rc io.ReadCloser
		if len(sc.body) > 0 {
			br = bytes.NewReader(sc.body)
			rc = io.NopCloser(br)
		}
		i := 0
		for pb.Next() {
			req := reqs[i%len(reqs)]
			if br != nil {
				br.Reset(sc.body)
				req.Body = rc
				req.ContentLength = int64(len(sc.body))
			}
			t.h.ServeHTTP(w, req)
			i++
		}
	})
}

func (t *httpTarget) probe(sc scenario) (int, string, error) {
	var body io.Reader
	if len(sc.body) > 0 {
		body = bytes.NewReader(sc.body)
	}
	req := httptest.NewRequest(sc.method, sc.targets[0], body)
	for k, v := range sc.headers {
		req.Header.Set(k, v)
	}
	if len(sc.body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), nil
}

// noopMiddleware count for the middleware scenario, shared by all adapters.
const middlewareCount = 5

// wrapPlainMiddlewares wraps h with n no-op func(http.Handler) http.Handler
// middlewares (used by stdlib-style frameworks without their own middleware
// types).
func wrapPlainMiddlewares(h http.Handler, n int) http.Handler {
	for i := 0; i < n; i++ {
		next := h
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
	return h
}

// -----------------------------------------------------------------------------
// Sanity test: every framework must answer every scenario with the expected
// status before its numbers mean anything.
// -----------------------------------------------------------------------------

func TestScenarioSanity(t *testing.T) {
	for _, e := range sortedRegistry() {
		e := e
		t.Run(e.t.name(), func(t *testing.T) {
			for _, sc := range allScenarios {
				if reason, ok := e.skips[sc.key]; ok {
					t.Logf("skip %s: %s", sc.key, reason)
					continue
				}
				status, body, err := e.t.probe(sc)
				if err != nil {
					t.Fatalf("%s: probe error: %v", sc.key, err)
				}
				if status != sc.wantStatus {
					t.Errorf("%s: status = %d, want %d (body: %.200s)", sc.key, status, sc.wantStatus, body)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Benchmark entry points, grouped by scenario
// -----------------------------------------------------------------------------

func runScenario(b *testing.B, sc scenario) {
	for _, e := range sortedRegistry() {
		e := e
		b.Run(e.t.name(), func(b *testing.B) {
			if reason, ok := e.skips[sc.key]; ok {
				b.Skip(reason)
			}
			e.t.bench(b, sc)
		})
	}
}

func runScenarioParallel(b *testing.B, sc scenario) {
	for _, e := range sortedRegistry() {
		e := e
		b.Run(e.t.name(), func(b *testing.B) {
			if reason, ok := e.skips[sc.key]; ok {
				b.Skip(reason)
			}
			e.t.benchParallel(b, sc)
		})
	}
}

// BenchmarkStaticRoute measures a bare static route: router match + tiny body.
func BenchmarkStaticRoute(b *testing.B) { runScenario(b, scStatic) }

// BenchmarkPathParam1 measures matching + extracting a single path parameter.
func BenchmarkPathParam1(b *testing.B) { runScenario(b, scParam1) }

// BenchmarkPathParam5 measures a deep route with five path parameters.
func BenchmarkPathParam5(b *testing.B) { runScenario(b, scParam5) }

// BenchmarkWildcard measures catch-all/wildcard route matching.
func BenchmarkWildcard(b *testing.B) { runScenario(b, scWildcard) }

// BenchmarkQueryParams measures URL query parsing (3 params).
func BenchmarkQueryParams(b *testing.B) { runScenario(b, scQuery) }

// BenchmarkJSONBind measures POST body decode into a struct + JSON response.
func BenchmarkJSONBind(b *testing.B) { runScenario(b, scJSONBind) }

// BenchmarkJSONResponse measures serializing a medium (~12 field) struct.
func BenchmarkJSONResponse(b *testing.B) { runScenario(b, scJSONResp) }

// BenchmarkMiddleware5 measures a static route behind 5 no-op middlewares.
func BenchmarkMiddleware5(b *testing.B) { runScenario(b, scMiddleware) }

// BenchmarkFullChain measures the whole request pipeline at once:
// path param + 2 query params + 2 headers + JSON body bind + JSON response.
func BenchmarkFullChain(b *testing.B) { runScenario(b, scFullChain) }

// BenchmarkRouteScale200 measures lookup cost with 200 registered routes,
// cycling static and parameterized hits.
func BenchmarkRouteScale200(b *testing.B) { runScenario(b, scScale) }

// BenchmarkNotFound measures the 404 path (router miss).
func BenchmarkNotFound(b *testing.B) { runScenario(b, scNotFound) }

// BenchmarkStaticRouteParallel exercises the static route under RunParallel.
func BenchmarkStaticRouteParallel(b *testing.B) { runScenarioParallel(b, scStatic) }

// BenchmarkFullChainParallel exercises the full pipeline under RunParallel.
func BenchmarkFullChainParallel(b *testing.B) { runScenarioParallel(b, scFullChain) }

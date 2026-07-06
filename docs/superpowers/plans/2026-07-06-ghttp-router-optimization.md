# ghttp Router Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an evidence-driven router optimization track for `ghttp`: first measure lookup accurately, then apply low-risk optimizations to the current `RadixRouter`, then add two independent experimental routers for direct comparison.

**Architecture:** Keep the current `Router` interface unchanged. Improve the current `RadixRouter` only in small, behavior-preserving ways; add `CompiledRouter` as a segment-array matcher; add `MatchitRouter` as a separate byte-prefix radix matcher without modifying `ghttp/radix.go` or replacing the default router.

**Tech Stack:** Go `net/http`, existing `ghttp.Router` interface, existing route syntax normalization helpers, existing `pathParamList`, standard `testing` benchmarks, no new dependencies.

## Global Constraints

- Always respond in Chinese-simplified when reporting progress.
- Format touched Go files with `gofmt`.
- Do not add external dependencies for router experiments.
- Preserve current public route syntax: static segments, `{param}`, deprecated `:param`, `*wildcard`, and `{path...}` through existing normalization helpers.
- Preserve current matching precedence unless a task explicitly adds tests for a different experimental router: static before param before wildcard.
- Do not make `MatchitRouter` the default router in this plan.
- Do not modify `ghttp/radix.go` while implementing `MatchitRouter`; it must live in new files so benchmark comparisons are meaningful.
- Run targeted package tests and benchmarks after each task.

---

## Scope

Implement now:
- Accurate router lookup and `ServeHTTP` benchmarks that avoid `httptest.NewRecorder()` allocation noise.
- Low-risk `RadixRouter` improvements that keep existing behavior and file ownership.
- An exported `NewCompiledRouter()` implementation for segment-array comparison.
- An exported `NewMatchitRouter()` implementation for independent byte-prefix radix comparison.
- A benchmark matrix comparing `RadixRouter`, `CompiledRouter`, `MatchitRouter`, and `StdRouter`.
- A short local benchmark summary document with measured numbers and the recommended next action.

Do not implement now:
- Switching `ghttp.New()` to use a new default router.
- Route constraints, regex params, host routing, version routing, or content negotiation routing.
- A public trailing-slash policy API.
- Any new dependency on third-party router packages.

## File Structure

- Modify: `ghttp/benchmark_test.go`
  - Owns baseline lookup benchmarks and cross-router comparison benchmarks.
- Modify: `ghttp/radix_router.go`
  - Owns low-risk `RadixRouter` lookup short-circuit changes.
- Modify: `ghttp/radix.go`
  - Only for current `RadixRouter` micro-optimizations. Do not touch this file for `MatchitRouter`.
- Test: `ghttp/router_test.go`
  - Existing router behavior tests remain the shared parity guard for `RadixRouter`.
- Create: `ghttp/compiled_router.go`
  - Owns the segment-array experimental router implementing `Router`.
- Create: `ghttp/compiled_router_test.go`
  - Owns behavior parity tests for `CompiledRouter`.
- Create: `ghttp/matchit_router.go`
  - Owns the standalone byte-prefix radix experimental router implementing `Router`.
- Create: `ghttp/matchit_router_test.go`
  - Owns behavior parity and conflict tests for `MatchitRouter`.
- Create: `docs/benchmark-results/ghttp-router-optimization.md`
  - Records benchmark commands, machine metadata from benchmark output, measured results, and recommendation.

## Shared Benchmark Command Set

Use these commands after relevant tasks:

```powershell
go test ./ghttp -count=1
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=3
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations' -benchmem -count=3
```

Expected successful benchmark output includes:
- `PASS`
- `ok   github.com/sofiworker/gk/ghttp`
- `0 B/op` and `0 allocs/op` for pure lookup benchmarks.

---

### Task 1: Accurate Router Lookup Benchmarks

**Files:**
- Modify: `ghttp/benchmark_test.go`

**Interfaces:**
- Consumes: `NewRadixRouter() *RadixRouter`, `(*RadixRouter).lookup(method, path string, params *pathParamList) *routeEntry`, `discardResponseWriter` from `router_test.go`.
- Produces: `BenchmarkRadixRouterLookup`, `BenchmarkRadixRouterServeHTTPNoopWriter`, `newBenchmarkRadixRouter(routeCount int) *RadixRouter`, `benchmarkLookupPaths(routeCount int) []benchmarkLookupPath`.

- [ ] **Step 1: Add benchmark sink variables**

Add these near the imports in `ghttp/benchmark_test.go`:

```go
var (
	benchmarkRouteEntrySink *routeEntry
	benchmarkParamLenSink   int
)
```

- [ ] **Step 2: Add direct lookup benchmark**

Add this function after `BenchmarkRadixRouter`:

```go
func BenchmarkRadixRouterLookup(b *testing.B) {
	for _, routeCount := range []int{16, 128, 1024, 8192} {
		router := newBenchmarkRadixRouter(routeCount)
		benchPaths := benchmarkLookupPaths(routeCount)

		for _, bp := range benchPaths {
			b.Run(fmt.Sprintf("routes=%d/%s", routeCount, bp.name), func(b *testing.B) {
				var params pathParamList
				entry := router.lookup(http.MethodGet, bp.path, &params)
				if bp.wantMatch && entry == nil {
					b.Fatalf("lookup(%q) returned nil", bp.path)
				}
				if !bp.wantMatch && entry != nil {
					b.Fatalf("lookup(%q) returned a route", bp.path)
				}

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					entry = router.lookup(http.MethodGet, bp.path, &params)
					benchmarkRouteEntrySink = entry
					benchmarkParamLenSink += params.Len()
				}
			})
		}
	}
}
```

- [ ] **Step 3: Add no-op writer ServeHTTP benchmark**

Add this function after `BenchmarkRadixRouterLookup`:

```go
func BenchmarkRadixRouterServeHTTPNoopWriter(b *testing.B) {
	for _, routeCount := range []int{16, 128, 1024, 8192} {
		router := newBenchmarkRadixRouter(routeCount)
		benchPaths := benchmarkLookupPaths(routeCount)
		writer := discardResponseWriter{}

		for _, bp := range benchPaths {
			if !bp.wantMatch {
				continue
			}
			b.Run(fmt.Sprintf("routes=%d/%s", routeCount, bp.name), func(b *testing.B) {
				req := httptest.NewRequest(http.MethodGet, bp.path, nil)

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					router.ServeHTTP(writer, req)
				}
			})
		}
	}
}
```

- [ ] **Step 4: Add benchmark route generator helpers**

Add these helpers before `BenchmarkStdRouter`:

```go
type benchmarkLookupPath struct {
	name      string
	path      string
	wantMatch bool
}

func newBenchmarkRadixRouter(routeCount int) *RadixRouter {
	router := NewRadixRouter()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for i := 0; i < routeCount; i++ {
		path := benchmarkRoutePattern(i)
		if err := router.Register(http.MethodGet, path, handler); err != nil {
			panic(err)
		}
	}

	return router
}

func benchmarkLookupPaths(routeCount int) []benchmarkLookupPath {
	return []benchmarkLookupPath{
		{name: "static", path: benchmarkRouteRequestPath(routeCount, 0), wantMatch: true},
		{name: "param", path: benchmarkRouteRequestPath(routeCount, 1), wantMatch: true},
		{name: "deep-param", path: benchmarkRouteRequestPath(routeCount, 2), wantMatch: true},
		{name: "wildcard", path: benchmarkRouteRequestPath(routeCount, 3), wantMatch: true},
		{name: "miss", path: "/missing/not-found", wantMatch: false},
	}
}

func benchmarkRoutePattern(index int) string {
	switch index % 4 {
	case 0:
		return fmt.Sprintf("/static/%04d", index)
	case 1:
		return fmt.Sprintf("/users/%04d/{id}", index)
	case 2:
		return fmt.Sprintf("/api/%04d/orgs/{orgID}/users/{userID}/posts/{postID}", index)
	default:
		return fmt.Sprintf("/assets/%04d/*path", index)
	}
}

func benchmarkRouteRequestPath(routeCount, kind int) string {
	index := benchmarkRouteIndex(routeCount, kind)
	switch kind {
	case 0:
		return fmt.Sprintf("/static/%04d", index)
	case 1:
		return fmt.Sprintf("/users/%04d/42", index)
	case 2:
		return fmt.Sprintf("/api/%04d/orgs/acme/users/alice/posts/99", index)
	default:
		return fmt.Sprintf("/assets/%04d/css/app.css", index)
	}
}

func benchmarkRouteIndex(routeCount, kind int) int {
	if routeCount <= 0 {
		return kind
	}

	index := routeCount / 2
	for index < routeCount && index%4 != kind {
		index++
	}
	if index < routeCount {
		return index
	}

	index = routeCount/2 - 1
	for index >= 0 && index%4 != kind {
		index--
	}
	if index >= 0 {
		return index
	}

	return kind
}
```

- [ ] **Step 5: Format and run benchmarks**

Run:

```powershell
gofmt -w ghttp\benchmark_test.go
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=1
go test ./ghttp -count=1
```

Expected:
- Benchmark command exits `0`.
- `BenchmarkRadixRouterLookup` reports `0 B/op` and `0 allocs/op`.
- Package test exits `0`.

- [ ] **Step 6: Commit**

```powershell
git add ghttp/benchmark_test.go
git commit -m "test(ghttp): add focused router lookup benchmarks"
```

---

### Task 2: Low-Risk Current RadixRouter Improvements

**Files:**
- Modify: `ghttp/radix_router.go`
- Optional Modify: `ghttp/radix.go`
- Test: `ghttp/router_test.go`
- Benchmark: `ghttp/benchmark_test.go`

**Interfaces:**
- Consumes: existing `RadixRouter`, `MethodMatcher`, `CompressedRadixTree`.
- Produces: behavior-preserving improvements to `MethodMatcher.lookup` and optional iterative tree lookup. No public API changes.

- [ ] **Step 1: Write a benchmark note before modifying code**

Record the Task 1 numbers in the commit message notes or PR notes. Include at least:

```text
Baseline command:
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=3

Cases to compare:
- routes=16/static
- routes=8192/static
- routes=8192/param
- routes=8192/deep-param
- routes=8192/wildcard
- routes=8192/miss
```

- [ ] **Step 2: Skip segment counting when no parameter routes exist**

Change `MethodMatcher.lookup` in `ghttp/radix_router.go` from unconditional segment counting:

```go
segCount := pathSegmentCount(path)

if tree, ok := m.segmentIndex[segCount]; ok {
	params.Reset()
	if entry := tree.lookup(path, params); entry != nil {
		return entry
	}
}
```

to:

```go
if len(m.segmentIndex) > 0 {
	segCount := pathSegmentCount(path)
	if tree, ok := m.segmentIndex[segCount]; ok {
		params.Reset()
		if entry := tree.lookup(path, params); entry != nil {
			return entry
		}
	}
}
```

This preserves behavior and only avoids `pathSegmentCount` for method groups with static routes and no parameter routes.

- [ ] **Step 3: Run behavior tests**

Run:

```powershell
go test ./ghttp -run 'TestRadixRouter|TestRouteParam|TestConvertPathParams' -count=1
```

Expected:
- Exit code `0`.
- No changed route precedence.

- [ ] **Step 4: Run benchmark comparison**

Run:

```powershell
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=3
```

Expected:
- Exit code `0`.
- Static-only cases should not regress.
- Mixed dynamic cases may be unchanged; record that result.

- [ ] **Step 5: Optional iterative lookup experiment**

Only do this step if Step 4 shows the previous change is too small to matter and all tests are passing.

Add a private iterative lookup to `ghttp/radix.go` while keeping `lookupRecursive` available until tests pass:

```go
func (t *CompressedRadixTree) lookup(path string, params *pathParamList) *routeEntry {
	return t.lookupRecursive(t.root, path, 0, params)
}
```

Replace it only after implementing an equivalent iterative traversal with explicit backtracking for static, param, and wildcard precedence. The iterative version must pass the existing tests in Step 3 and preserve `params.Truncate` behavior on failed param branches. If the iterative version needs more than one small helper or becomes harder to audit than the recursive version, stop this optional step and keep the recursive implementation.

- [ ] **Step 6: Format, test, and commit**

Run:

```powershell
gofmt -w ghttp\radix_router.go ghttp\radix.go
go test ./ghttp -count=1
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=3
```

Commit:

```powershell
git add ghttp/radix_router.go ghttp/radix.go
git commit -m "perf(ghttp): trim avoidable radix lookup work"
```

---

### Task 3: Compiled Segment Router Prototype

**Files:**
- Create: `ghttp/compiled_router.go`
- Create: `ghttp/compiled_router_test.go`
- Modify: `ghttp/benchmark_test.go`

**Interfaces:**
- Consumes: `Router`, `routeEntry`, `newRouteEntry`, `normalizeRoutePath`, `splitPathSegments`, `pathParamList`, `serveRouteEntry`, `allowedMethods` semantics from `RadixRouter`.
- Produces: `type CompiledRouter struct`, `func NewCompiledRouter() *CompiledRouter`, `func (r *CompiledRouter) Register(method, path string, handler http.Handler) error`, `func (r *CompiledRouter) ServeHTTP(w http.ResponseWriter, req *http.Request)`.

- [ ] **Step 1: Write parity tests for static, param, wildcard, HEAD fallback, and 405**

Create `ghttp/compiled_router_test.go`:

```go
package ghttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompiledRouterStaticParamWildcard(t *testing.T) {
	r := NewCompiledRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, route := range []string{
		"/users",
		"/users/{id}",
		"/assets/*path",
		"/articles/{category}/{id}",
	} {
		if err := r.Register(http.MethodGet, route, dummy); err != nil {
			t.Fatalf("Register(%q) failed: %v", route, err)
		}
	}

	tests := []struct {
		path string
		want int
	}{
		{"/users", http.StatusOK},
		{"/users/42", http.StatusOK},
		{"/assets/img/logo.png", http.StatusOK},
		{"/articles/tech/123", http.StatusOK},
		{"/notfound", http.StatusNotFound},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		r.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Fatalf("GET %s status = %d, want %d", tt.path, rec.Code, tt.want)
		}
	}
}

func TestCompiledRouterPathParams(t *testing.T) {
	r := NewCompiledRouter()
	var captured pathParamList
	if err := r.Register(http.MethodGet, "/users/{id}", pathParamHandlerFunc(func(w http.ResponseWriter, req *http.Request, params pathParamList) {
		captured = params
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured.Get("id") != "42" {
		t.Fatalf("id = %q, want 42", captured.Get("id"))
	}
}

func TestCompiledRouterHEADFallsBackToGETWithoutBody(t *testing.T) {
	r := NewCompiledRouter()
	if err := r.Register(http.MethodGet, "/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Health", "ok")
		_, _ = w.Write([]byte("healthy"))
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/health", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Health") != "ok" {
		t.Fatalf("X-Health = %q, want ok", rec.Header().Get("X-Health"))
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0", rec.Body.Len())
	}
}

func TestCompiledRouterMethodNotAllowedIncludesAllow(t *testing.T) {
	r := NewCompiledRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if err := r.Register(http.MethodGet, "/users/{id}", dummy); err != nil {
		t.Fatalf("Register GET failed: %v", err)
	}
	if err := r.Register(http.MethodPut, "/users/{id}", dummy); err != nil {
		t.Fatalf("Register PUT failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/42", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	allow := rec.Header().Get("Allow")
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut} {
		if !strings.Contains(allow, method) {
			t.Fatalf("Allow = %q, want method %s", allow, method)
		}
	}
}
```

- [ ] **Step 2: Run tests and confirm failure**

Run:

```powershell
go test ./ghttp -run 'TestCompiledRouter' -count=1
```

Expected:
- Fails to compile with `undefined: NewCompiledRouter`.

- [ ] **Step 3: Add the compiled router skeleton**

Create `ghttp/compiled_router.go`:

```go
package ghttp

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type CompiledRouter struct {
	methods map[string]*compiledMethod
}

type compiledMethod struct {
	static map[string]*routeEntry
	roots  map[int]int
	nodes  []compiledNode
	paths  map[string]struct{}
}

type compiledNode struct {
	staticChildren []compiledStaticChild
	paramChild     int
	paramName      string
	wildcardChild  int
	entry          *routeEntry
}

type compiledStaticChild struct {
	segment string
	index   int
}

func NewCompiledRouter() *CompiledRouter {
	return &CompiledRouter{methods: make(map[string]*compiledMethod)}
}
```

- [ ] **Step 4: Implement registration**

Add these methods to `ghttp/compiled_router.go`:

```go
func (r *CompiledRouter) Register(method, path string, handler http.Handler) error {
	method = strings.ToUpper(method)
	m := r.methods[method]
	if m == nil {
		m = &compiledMethod{
			static: make(map[string]*routeEntry),
			roots:  make(map[int]int),
			paths:  make(map[string]struct{}),
		}
		r.methods[method] = m
	}

	path = normalizeRoutePath(path)
	if _, exists := m.paths[path]; exists {
		return fmt.Errorf("%w: route %s already registered", ErrConflict, path)
	}
	m.paths[path] = struct{}{}

	entry := newRouteEntry(path, handler)
	if !strings.Contains(path, ":") && !strings.Contains(path, "*") {
		m.static[path] = entry
		return nil
	}

	segments := splitPathSegments(path)
	root, ok := m.roots[len(segments)]
	if !ok {
		root = m.addNode()
		m.roots[len(segments)] = root
	}
	m.insert(root, segments, entry)
	return nil
}

func (m *compiledMethod) addNode() int {
	m.nodes = append(m.nodes, compiledNode{
		paramChild:    -1,
		wildcardChild: -1,
	})
	return len(m.nodes) - 1
}

func (m *compiledMethod) insert(root int, segments []string, entry *routeEntry) {
	nodeIndex := root
	for i, segment := range segments {
		node := &m.nodes[nodeIndex]
		last := i == len(segments)-1

		if strings.HasPrefix(segment, "*") {
			child := m.addNode()
			node.wildcardChild = child
			m.nodes[child].paramName = strings.TrimPrefix(segment, "*")
			m.nodes[child].entry = entry
			return
		}

		if strings.HasPrefix(segment, ":") {
			if node.paramChild < 0 {
				node.paramChild = m.addNode()
			}
			child := node.paramChild
			m.nodes[child].paramName = strings.TrimPrefix(segment, ":")
			if last {
				m.nodes[child].entry = entry
			}
			nodeIndex = child
			continue
		}

		child := node.findStaticChild(segment)
		if child < 0 {
			child = m.addNode()
			node.staticChildren = append(node.staticChildren, compiledStaticChild{segment: segment, index: child})
		}
		if last {
			m.nodes[child].entry = entry
		}
		nodeIndex = child
	}

	if len(segments) == 0 {
		m.nodes[root].entry = entry
	}
}

func (n *compiledNode) findStaticChild(segment string) int {
	for _, child := range n.staticChildren {
		if child.segment == segment {
			return child.index
		}
	}
	return -1
}
```

- [ ] **Step 5: Implement lookup and serving**

Add lookup and HTTP methods:

```go
func (r *CompiledRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	method := req.Method
	path := strings.TrimRight(req.URL.Path, "/")
	if path == "" {
		path = "/"
	}

	var params pathParamList
	if method == http.MethodHead {
		if entry := r.lookup(http.MethodHead, path, &params); entry != nil {
			serveRouteEntry(headResponseWriter{ResponseWriter: w}, req, entry, params)
			return
		}
		if entry := r.lookup(http.MethodGet, path, &params); entry != nil {
			serveRouteEntry(headResponseWriter{ResponseWriter: w}, req, entry, params)
			return
		}
	} else if entry := r.lookup(method, path, &params); entry != nil {
		serveRouteEntry(w, req, entry, params)
		return
	}

	allowed := r.allowedMethods(path)
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (r *CompiledRouter) lookup(method, path string, params *pathParamList) *routeEntry {
	m := r.methods[method]
	if m == nil {
		params.Reset()
		return nil
	}
	return m.lookup(path, params)
}

func (m *compiledMethod) lookup(path string, params *pathParamList) *routeEntry {
	if entry := m.static[path]; entry != nil {
		params.Reset()
		return entry
	}

	segments := splitPathSegments(path)
	root, ok := m.roots[len(segments)]
	if !ok {
		params.Reset()
		return nil
	}

	params.Reset()
	if entry := m.lookupNode(root, segments, 0, params); entry != nil {
		return entry
	}
	params.Reset()
	return nil
}

func (m *compiledMethod) lookupNode(nodeIndex int, segments []string, depth int, params *pathParamList) *routeEntry {
	node := &m.nodes[nodeIndex]
	if depth == len(segments) {
		return node.entry
	}

	segment := segments[depth]
	if child := node.findStaticChild(segment); child >= 0 {
		if entry := m.lookupNode(child, segments, depth+1, params); entry != nil {
			return entry
		}
	}

	if node.paramChild >= 0 {
		child := &m.nodes[node.paramChild]
		paramLen := params.Len()
		params.Add(child.paramName, segment)
		if entry := m.lookupNode(node.paramChild, segments, depth+1, params); entry != nil {
			return entry
		}
		params.Truncate(paramLen)
	}

	if node.wildcardChild >= 0 {
		child := &m.nodes[node.wildcardChild]
		params.Add(child.paramName, strings.Join(segments[depth:], "/"))
		return child.entry
	}

	return nil
}

func (r *CompiledRouter) allowedMethods(path string) []string {
	seen := make(map[string]struct{})
	var allowed []string
	add := func(method string) {
		if _, ok := seen[method]; ok {
			return
		}
		seen[method] = struct{}{}
		allowed = append(allowed, method)
	}

	for _, method := range allHTTPMethods {
		var params pathParamList
		if r.lookup(method, path, &params) == nil {
			continue
		}
		add(method)
		if method == http.MethodGet {
			add(http.MethodHead)
		}
	}

	var custom []string
	for method := range r.methods {
		if _, ok := seen[method]; ok || isStandardHTTPMethod(method) {
			continue
		}
		var params pathParamList
		if r.lookup(method, path, &params) != nil {
			custom = append(custom, method)
		}
	}
	sort.Strings(custom)
	for _, method := range custom {
		add(method)
	}

	return allowed
}
```

- [ ] **Step 6: Run tests and benchmark**

Run:

```powershell
gofmt -w ghttp\compiled_router.go ghttp\compiled_router_test.go
go test ./ghttp -run 'TestCompiledRouter' -count=1
go test ./ghttp -count=1
```

Expected:
- `TestCompiledRouter...` tests pass.
- Full `ghttp` package tests pass.

- [ ] **Step 7: Add compiled router benchmark comparison**

Modify `ghttp/benchmark_test.go` by adding:

```go
func BenchmarkRouterImplementations(b *testing.B) {
	for _, routeCount := range []int{128, 1024, 8192} {
		for _, impl := range []struct {
			name string
			make func(int) Router
		}{
			{name: "radix", make: func(n int) Router { return newBenchmarkRadixRouter(n) }},
			{name: "compiled", make: func(n int) Router { return newBenchmarkCompiledRouter(n) }},
			{name: "std", make: func(n int) Router { return newBenchmarkStdRouter(n) }},
		} {
			router := impl.make(routeCount)
			for _, bp := range benchmarkLookupPaths(routeCount) {
				if !bp.wantMatch {
					continue
				}
				b.Run(fmt.Sprintf("%s/routes=%d/%s", impl.name, routeCount, bp.name), func(b *testing.B) {
					req := httptest.NewRequest(http.MethodGet, bp.path, nil)
					w := discardResponseWriter{}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						router.ServeHTTP(w, req)
					}
				})
			}
		}
	}
}

func newBenchmarkCompiledRouter(routeCount int) *CompiledRouter {
	router := NewCompiledRouter()
	registerBenchmarkRoutes(router, routeCount)
	return router
}

func newBenchmarkStdRouter(routeCount int) *StdRouter {
	router := NewStdRouter()
	registerBenchmarkRoutes(router, routeCount)
	return router
}

func registerBenchmarkRoutes(router Router, routeCount int) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	for i := 0; i < routeCount; i++ {
		if err := router.Register(http.MethodGet, benchmarkRoutePattern(i), handler); err != nil {
			panic(err)
		}
	}
}
```

Then update `newBenchmarkRadixRouter` to call `registerBenchmarkRoutes`:

```go
func newBenchmarkRadixRouter(routeCount int) *RadixRouter {
	router := NewRadixRouter()
	registerBenchmarkRoutes(router, routeCount)
	return router
}
```

- [ ] **Step 8: Run comparison and commit**

Run:

```powershell
gofmt -w ghttp\benchmark_test.go
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations' -benchmem -count=3
```

Commit:

```powershell
git add ghttp/compiled_router.go ghttp/compiled_router_test.go ghttp/benchmark_test.go
git commit -m "feat(ghttp): add compiled segment router experiment"
```

---

### Task 4: Standalone Matchit-Style Router

**Files:**
- Create: `ghttp/matchit_router.go`
- Create: `ghttp/matchit_router_test.go`
- Modify: `ghttp/benchmark_test.go`

**Interfaces:**
- Consumes: `Router`, `routeEntry`, `newRouteEntry`, `normalizeRoutePath`, `pathParamList`, `serveRouteEntry`, `headResponseWriter`, `allHTTPMethods`.
- Produces: `type MatchitRouter struct`, `func NewMatchitRouter() *MatchitRouter`, `func (r *MatchitRouter) Register(method, path string, handler http.Handler) error`, `func (r *MatchitRouter) ServeHTTP(w http.ResponseWriter, req *http.Request)`.

**Important isolation rule:** This task must not modify `ghttp/radix.go`, `ghttp/radix_router.go`, or the behavior of `NewRadixRouter()`. The point is to compare two separate implementations.

- [ ] **Step 1: Write parity tests**

Create `ghttp/matchit_router_test.go`:

```go
package ghttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMatchitRouterStaticParamWildcard(t *testing.T) {
	r := NewMatchitRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, route := range []string{
		"/users",
		"/users/{id}",
		"/assets/*path",
		"/articles/{category}/{id}",
	} {
		if err := r.Register(http.MethodGet, route, dummy); err != nil {
			t.Fatalf("Register(%q) failed: %v", route, err)
		}
	}

	tests := []struct {
		path string
		want int
	}{
		{"/users", http.StatusOK},
		{"/users/42", http.StatusOK},
		{"/assets/img/logo.png", http.StatusOK},
		{"/articles/tech/123", http.StatusOK},
		{"/notfound", http.StatusNotFound},
	}

	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		r.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Fatalf("GET %s status = %d, want %d", tt.path, rec.Code, tt.want)
		}
	}
}

func TestMatchitRouterStaticBeatsParam(t *testing.T) {
	r := NewMatchitRouter()
	if err := r.Register(http.MethodGet, "/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("param"))
	})); err != nil {
		t.Fatalf("Register param failed: %v", err)
	}
	if err := r.Register(http.MethodGet, "/users/search", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("static"))
	})); err != nil {
		t.Fatalf("Register static failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/search", nil)
	r.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "static" {
		t.Fatalf("body = %q, want static", got)
	}
}

func TestMatchitRouterPathParams(t *testing.T) {
	r := NewMatchitRouter()
	var captured pathParamList
	if err := r.Register(http.MethodGet, "/orgs/{orgID}/users/{userID}", pathParamHandlerFunc(func(w http.ResponseWriter, req *http.Request, params pathParamList) {
		captured = params
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orgs/acme/users/alice", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured.Get("orgID") != "acme" || captured.Get("userID") != "alice" {
		t.Fatalf("params orgID=%q userID=%q, want acme/alice", captured.Get("orgID"), captured.Get("userID"))
	}
}

func TestMatchitRouterRejectsDuplicateRoute(t *testing.T) {
	r := NewMatchitRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if err := r.Register(http.MethodGet, "/users/{id}", dummy); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	if err := r.Register(http.MethodGet, "/users/{id}", dummy); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Register error = %v, want ErrConflict", err)
	}
}
```

- [ ] **Step 2: Run tests and confirm failure**

Run:

```powershell
go test ./ghttp -run 'TestMatchitRouter' -count=1
```

Expected:
- Fails to compile with `undefined: NewMatchitRouter`.

- [ ] **Step 3: Add router structs and constructor**

Create `ghttp/matchit_router.go`:

```go
package ghttp

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type MatchitRouter struct {
	methods map[string]*matchitMethod
}

type matchitMethod struct {
	root  *matchitNode
	paths map[string]struct{}
}

type matchitNode struct {
	prefix        string
	staticChild   []*matchitNode
	paramChild    *matchitNode
	paramName     string
	wildcardChild *matchitNode
	entry         *routeEntry
}

func NewMatchitRouter() *MatchitRouter {
	return &MatchitRouter{methods: make(map[string]*matchitMethod)}
}
```

- [ ] **Step 4: Implement registration as an independent byte-prefix tree**

Add registration and insertion:

```go
func (r *MatchitRouter) Register(method, path string, handler http.Handler) error {
	method = strings.ToUpper(method)
	m := r.methods[method]
	if m == nil {
		m = &matchitMethod{
			root:  &matchitNode{},
			paths: make(map[string]struct{}),
		}
		r.methods[method] = m
	}

	path = normalizeRoutePath(path)
	if _, exists := m.paths[path]; exists {
		return fmt.Errorf("%w: route %s already registered", ErrConflict, path)
	}
	m.paths[path] = struct{}{}
	m.insert(path, newRouteEntry(path, handler))
	return nil
}

func (m *matchitMethod) insert(path string, entry *routeEntry) {
	segments := splitPathSegments(path)
	node := m.root
	if len(segments) == 0 {
		node.entry = entry
		return
	}

	for i, segment := range segments {
		last := i == len(segments)-1

		if strings.HasPrefix(segment, "*") {
			if node.wildcardChild == nil {
				node.wildcardChild = &matchitNode{prefix: segment}
			}
			node.wildcardChild.paramName = strings.TrimPrefix(segment, "*")
			node.wildcardChild.entry = entry
			return
		}

		if strings.HasPrefix(segment, ":") {
			if node.paramChild == nil {
				node.paramChild = &matchitNode{prefix: segment}
			}
			node.paramChild.paramName = strings.TrimPrefix(segment, ":")
			if last {
				node.paramChild.entry = entry
			}
			node = node.paramChild
			continue
		}

		child := node.findStaticChild(segment)
		if child == nil {
			child = &matchitNode{prefix: segment}
			node.staticChild = append(node.staticChild, child)
		}
		if last {
			child.entry = entry
		}
		node = child
	}
}

func (n *matchitNode) findStaticChild(segment string) *matchitNode {
	for _, child := range n.staticChild {
		if child.prefix == segment {
			return child
		}
	}
	return nil
}
```

This first implementation is segment-based but lives outside the current radix tree. After the parity tests pass, a later refinement inside this same `MatchitRouter` can split static child prefixes by common byte prefix. Do not move this code into `ghttp/radix.go`.

- [ ] **Step 5: Implement lookup and serving**

Add lookup and HTTP serving:

```go
func (r *MatchitRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	method := req.Method
	path := strings.TrimRight(req.URL.Path, "/")
	if path == "" {
		path = "/"
	}

	var params pathParamList
	if method == http.MethodHead {
		if entry := r.lookup(http.MethodHead, path, &params); entry != nil {
			serveRouteEntry(headResponseWriter{ResponseWriter: w}, req, entry, params)
			return
		}
		if entry := r.lookup(http.MethodGet, path, &params); entry != nil {
			serveRouteEntry(headResponseWriter{ResponseWriter: w}, req, entry, params)
			return
		}
	} else if entry := r.lookup(method, path, &params); entry != nil {
		serveRouteEntry(w, req, entry, params)
		return
	}

	allowed := r.allowedMethods(path)
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (r *MatchitRouter) lookup(method, path string, params *pathParamList) *routeEntry {
	m := r.methods[method]
	if m == nil {
		params.Reset()
		return nil
	}
	return m.lookup(path, params)
}

func (m *matchitMethod) lookup(path string, params *pathParamList) *routeEntry {
	params.Reset()
	segments := splitPathSegments(path)
	if len(segments) == 0 {
		return m.root.entry
	}
	entry := m.lookupNode(m.root, segments, 0, params)
	if entry == nil {
		params.Reset()
	}
	return entry
}

func (m *matchitMethod) lookupNode(node *matchitNode, segments []string, depth int, params *pathParamList) *routeEntry {
	if depth == len(segments) {
		return node.entry
	}

	segment := segments[depth]
	if child := node.findStaticChild(segment); child != nil {
		if entry := m.lookupNode(child, segments, depth+1, params); entry != nil {
			return entry
		}
	}

	if node.paramChild != nil {
		paramLen := params.Len()
		params.Add(node.paramChild.paramName, segment)
		if entry := m.lookupNode(node.paramChild, segments, depth+1, params); entry != nil {
			return entry
		}
		params.Truncate(paramLen)
	}

	if node.wildcardChild != nil {
		params.Add(node.wildcardChild.paramName, strings.Join(segments[depth:], "/"))
		return node.wildcardChild.entry
	}

	return nil
}

func (r *MatchitRouter) allowedMethods(path string) []string {
	seen := make(map[string]struct{})
	var allowed []string
	add := func(method string) {
		if _, ok := seen[method]; ok {
			return
		}
		seen[method] = struct{}{}
		allowed = append(allowed, method)
	}

	for _, method := range allHTTPMethods {
		var params pathParamList
		if r.lookup(method, path, &params) == nil {
			continue
		}
		add(method)
		if method == http.MethodGet {
			add(http.MethodHead)
		}
	}

	var custom []string
	for method := range r.methods {
		if _, ok := seen[method]; ok || isStandardHTTPMethod(method) {
			continue
		}
		var params pathParamList
		if r.lookup(method, path, &params) != nil {
			custom = append(custom, method)
		}
	}
	sort.Strings(custom)
	for _, method := range custom {
		add(method)
	}

	return allowed
}
```

- [ ] **Step 6: Run parity tests**

Run:

```powershell
gofmt -w ghttp\matchit_router.go ghttp\matchit_router_test.go
go test ./ghttp -run 'TestMatchitRouter' -count=1
go test ./ghttp -count=1
```

Expected:
- `TestMatchitRouter...` tests pass.
- Full `ghttp` package tests pass.

- [ ] **Step 7: Add MatchitRouter to comparison benchmark**

Modify `BenchmarkRouterImplementations` in `ghttp/benchmark_test.go` by adding `matchit`:

```go
{name: "matchit", make: func(n int) Router { return newBenchmarkMatchitRouter(n) }},
```

Add helper:

```go
func newBenchmarkMatchitRouter(routeCount int) *MatchitRouter {
	router := NewMatchitRouter()
	registerBenchmarkRoutes(router, routeCount)
	return router
}
```

- [ ] **Step 8: Run comparison and commit**

Run:

```powershell
gofmt -w ghttp\benchmark_test.go
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations' -benchmem -count=3
```

Commit:

```powershell
git add ghttp/matchit_router.go ghttp/matchit_router_test.go ghttp/benchmark_test.go
git commit -m "feat(ghttp): add standalone matchit-style router experiment"
```

---

### Task 5: Benchmark Report And Decision Gate

**Files:**
- Create: `docs/benchmark-results/ghttp-router-optimization.md`
- Modify: `ghttp/README.md` only if the report recommends exposing a new experimental router in user docs.

**Interfaces:**
- Consumes: benchmark output from Tasks 1-4.
- Produces: a written recommendation for whether to keep optimizing `RadixRouter`, continue `CompiledRouter`, continue `MatchitRouter`, or stop because typed handler overhead dominates.

- [ ] **Step 1: Run final benchmark set**

Run:

```powershell
go test ./ghttp -count=1
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=5
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations' -benchmem -count=5
```

Expected:
- All commands exit `0`.
- No router implementation allocates on matching hot paths except `StdRouter` where standard library path value extraction may allocate.

- [ ] **Step 2: Write benchmark report**

Create `docs/benchmark-results/ghttp-router-optimization.md`:

```markdown
# ghttp Router Optimization Benchmark Results

## Environment

Record the exact `goos`, `goarch`, package, and CPU lines from the final benchmark output.

## Commands

```powershell
go test ./ghttp -count=1
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=5
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations' -benchmem -count=5
```

## Results

Include the exact benchmark output lines for these cases:

- `BenchmarkRouterImplementations/radix/routes=8192/static`
- `BenchmarkRouterImplementations/radix/routes=8192/param`
- `BenchmarkRouterImplementations/radix/routes=8192/deep-param`
- `BenchmarkRouterImplementations/compiled/routes=8192/static`
- `BenchmarkRouterImplementations/compiled/routes=8192/param`
- `BenchmarkRouterImplementations/compiled/routes=8192/deep-param`
- `BenchmarkRouterImplementations/matchit/routes=8192/static`
- `BenchmarkRouterImplementations/matchit/routes=8192/param`
- `BenchmarkRouterImplementations/matchit/routes=8192/deep-param`
- `BenchmarkRouterImplementations/std/routes=8192/param`

Keep the raw `ns/op`, `B/op`, and `allocs/op` values in a fenced text block so future runs can be compared mechanically.

## Recommendation

Apply these decision rules and record the selected recommendation:

- Keep RadixRouter as default when experimental routers are not at least 15% faster on param and deep-param cases.
- Continue CompiledRouter if it is faster than RadixRouter on deep-param and wildcard cases without adding allocations.
- Continue MatchitRouter if it beats RadixRouter on static, param, and deep-param cases and the implementation stays smaller than 300 lines excluding tests.
- Stop router optimization and focus on typed handler binding when all router implementations are below 250 ns/op while full typed handler paths remain above 1 us/op.
```
```

Before committing, add one final sentence naming the chosen route and the measured reason.

- [ ] **Step 3: Run report self-check**

Run:

```powershell
rg -n "paste[ ]measured[ ]value|Choose[ ]one|T[B]D|TO[D]O" docs\benchmark-results\ghttp-router-optimization.md
```

Expected:
- No matches.

- [ ] **Step 4: Commit report**

```powershell
git add docs/benchmark-results/ghttp-router-optimization.md
git commit -m "docs(ghttp): record router optimization benchmark comparison"
```

---

## Execution Notes

- Run tasks in order. Task 4 can start after Task 1 if the goal is only to compare `MatchitRouter` against the existing `RadixRouter`, but the final report is stronger if Task 2 and Task 3 are included.
- `MatchitRouter` must stay separate from `RadixRouter`; do not reuse or edit `CompressedRadixTree`.
- If `CompiledRouter` or `MatchitRouter` becomes slower than current `RadixRouter` in `BenchmarkRouterImplementations`, keep the implementation for comparison only until Task 5 records the result, then decide whether to remove it in a separate cleanup plan.
- If a router implementation needs behavior that conflicts with existing route semantics, add the behavior as an explicit test in that router's own test file and document the divergence in the final report.

## Self-Review

- Spec coverage: The plan covers accurate benchmark baseline, current `RadixRouter` small optimization, a compiled segment router prototype, a standalone matchit-style router, and a final comparison report.
- Placeholder scan: The plan avoids unresolved placeholder values and includes an `rg` self-check for the generated report.
- Type consistency: Public constructors are `NewCompiledRouter()` and `NewMatchitRouter()`. Both return routers implementing the existing `Router` interface. Shared benchmark helpers use `Router.Register` and `Router.ServeHTTP`.

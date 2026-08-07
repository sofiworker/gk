package ghttp

// perf_probe_test.go decomposes the per-request typed-route overhead into
// measurable components, and benchmarks prototype alternatives against the
// current implementation. Temporary instrumentation for optimization work.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type probeInput struct {
	Params `json:"-"`

	Body struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}
}

func probeRequest() *http.Request {
	req := httptest.NewRequest("GET", "/users/42?page=1&sort=asc", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "probe/1.0")
	req.Header.Set("Cookie", "session_id=abc123; theme=dark")
	req.RemoteAddr = "10.0.0.1:5555"
	return req
}

var probePathParams = func() pathParamList {
	var p pathParamList
	p.Add("id", "42")
	return p
}()

// --- Component costs of the Params view (lazy since the Params rework) ---

func BenchmarkProbe_ParamsView_Construct(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = paramsFromRequestWithPathParams(req, nil, probePathParams)
	}
}

func BenchmarkProbe_HeaderClone(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = req.Header.Clone()
	}
}

func BenchmarkProbe_QueryParse(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = req.URL.Query()
	}
}

func BenchmarkProbe_QueryParseAndClone(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cloneQueryParams(req.URL.Query())
	}
}

func BenchmarkProbe_Cookies(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = req.Cookies()
	}
}

func BenchmarkProbe_CookiesAndClone(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cloneCookies(req.Cookies())
	}
}

func BenchmarkProbe_ClientIPResolve(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = defaultClientIPResolver(req)
	}
}

// --- Input construction in the typed handler path (compiled at registration) ---

func BenchmarkProbe_CompiledInputConstruct(b *testing.B) {
	ci := compileInput[probeInput]()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		target := ci.newTarget()
		_ = ci.finish(target)
	}
}

func BenchmarkProbe_CompiledInputConstructPtr(b *testing.B) {
	ci := compileInput[*probeInput]()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		target := ci.newTarget()
		_ = ci.finish(target)
	}
}

// --- Per-request codec resolve (vs storing codec at registration) ---

func BenchmarkProbe_CodecResolve(b *testing.B) {
	cm := NewCodecManager()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = cm.Resolve(MIMEJSON)
	}
}

// --- Default validator on a struct without validate tags ---

func BenchmarkProbe_ValidatorNoTags(b *testing.B) {
	v := newDefaultValidator()
	ctx := context.Background()
	in := &probeInput{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = v.Validate(ctx, in)
	}
}

// --- Accept negotiation per request (envelope path) ---

func BenchmarkProbe_Negotiate(b *testing.B) {
	cm := NewCodecManager()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cm.Negotiate("application/json")
	}
}

// --- Prototype: lazy Params holding the request, no upfront clones ---

type lazyParams struct {
	r     *http.Request
	path  pathParamList
	query url.Values // parsed on first access
}

func (p *lazyParams) Path(key string) string { return p.path.Get(key) }

func (p *lazyParams) Query(key string) string {
	if p.query == nil {
		p.query = p.r.URL.Query()
	}
	return p.query.Get(key)
}

func (p *lazyParams) Header(key string) string { return p.r.Header.Get(key) }

func (p *lazyParams) Cookie(key string) string {
	c, err := p.r.Cookie(key)
	if err != nil {
		return ""
	}
	return c.Value
}

// Construction cost only (what every request pays regardless of usage).
func BenchmarkProbe_ParamsLazy_Construct(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := lazyParams{r: req, path: probePathParams}
		_ = p
	}
}

// Typical usage: read one path param, one query param, one header.
func BenchmarkProbe_ParamsLazy_TypicalUse(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := lazyParams{r: req, path: probePathParams}
		_ = p.Path("id")
		_ = p.Query("page")
		_ = p.Header("Authorization")
	}
}

// Same typical usage via the real Params implementation, for apples-to-apples.
func BenchmarkProbe_ParamsView_TypicalUse(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := paramsFromRequestWithPathParams(req, nil, probePathParams)
		_ = p.Path("id")
		_ = p.Query("page")
		_ = p.Header("Authorization")
	}
}

// --- Prototype: precompiled input constructor (vs per-request reflection) ---

// What a registration-time compiled binder would do per request: one struct
// alloc, direct field writes. Measured as the floor for the typed path.
func BenchmarkProbe_PrecompiledConstruct(b *testing.B) {
	req := probeRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		in := &probeInput{}
		in.Params = Params{path: probePathParams, state: &paramsState{header: req.Header}}
		_ = in
	}
}

// --- Full in-process typed chain (no TCP), current implementation ---

func BenchmarkProbe_FullChainInProcess(b *testing.B) {
	s := New(WithProduces(MIMEJSON))

	type out struct {
		ID   string `json:"id"`
		Page string `json:"page"`
	}
	Route[probeInput, out](s).GET("/users/{id}").To(func(ctx context.Context, req probeInput) (out, error) {
		return out{ID: req.Path("id"), Page: req.Query("page")}, nil
	})
	// warm: finalize routes
	warm := httptest.NewRecorder()
	s.ServeHTTP(warm, probeRequest())
	if warm.Code != 200 {
		b.Fatalf("unexpected status %d: %s", warm.Code, warm.Body.String())
	}

	req := probeRequest()
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		s.ServeHTTP(w, req)
	}
}

// Raw handler via same router, as the framework-floor reference.
func BenchmarkProbe_FullChainRawHandler(b *testing.B) {
	s := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](s).GET("/users/{id}").ToRaw(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42"}`))
	})
	warm := httptest.NewRecorder()
	s.ServeHTTP(warm, probeRequest())

	req := probeRequest()
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		s.ServeHTTP(w, req)
	}
}

// POST with JSON body through the full typed chain, in-process.
func BenchmarkProbe_FullChainInProcessPost(b *testing.B) {
	s := New(WithProduces(MIMEJSON))

	type out struct {
		Name string `json:"name"`
	}
	Route[probeInput, out](s).POST("/users/{id}").To(func(ctx context.Context, req probeInput) (out, error) {
		return out{Name: req.Body.Name}, nil
	})
	body := `{"name":"Alice","email":"a@b.c","age":30}`
	mk := func() *http.Request {
		r := httptest.NewRequest("POST", "/users/42", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	warm := httptest.NewRecorder()
	s.ServeHTTP(warm, mk())
	if warm.Code != 200 {
		b.Fatalf("unexpected status %d: %s", warm.Code, warm.Body.String())
	}

	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		s.ServeHTTP(w, mk())
	}
}

func TestFullChainAllocBudget(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	Route[Params, struct {
		ID string `json:"id"`
	}](server).GET("/users/{id}").To(func(context.Context, Params) (struct {
		ID string `json:"id"`
	}, error) {
		return struct {
			ID string `json:"id"`
		}{ID: "42"}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	rec := httptest.NewRecorder()
	res := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			rec.Body.Reset()
			server.ServeHTTP(rec, req)
		}
	})
	allocs := res.AllocsPerOp()
	if allocs > 15 {
		t.Fatalf("allocs/op = %d, want <= 15 (final budget 8 after WS9 iterations)", allocs)
	}
}

var _ = reflect.TypeOf // keep reflect import if prototypes change

// --- Prototype: what the full typed chain costs if Params is lazy, input
// construction is precompiled, and the codec is cached at registration.
// Hand-written equivalent of the optimized pipeline, same router, same output.

func BenchmarkProbe_FullChainOptimizedPrototype(b *testing.B) {
	s := New(WithProduces(MIMEJSON))

	type out struct {
		ID   string `json:"id"`
		Page string `json:"page"`
	}
	codec, _ := s.codecMgr.Resolve(MIMEJSON) // resolved once at registration

	handler := func(ctx context.Context, p *lazyParams) (out, error) {
		return out{ID: p.Path("id"), Page: p.Query("page")}, nil
	}

	Route[struct{}, struct{}](s).GET("/users/{id}").ToHTTP(pathParamHandlerFunc(
		func(w http.ResponseWriter, r *http.Request, params pathParamList) {
			p := lazyParams{r: r, path: params}
			resp, err := handler(r.Context(), &p)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", MIMEJSON)
			w.WriteHeader(200)
			_ = codec.Marshal(w, &resp)
		}))

	warm := httptest.NewRecorder()
	s.ServeHTTP(warm, probeRequest())
	if warm.Code != 200 {
		b.Fatalf("unexpected status %d: %s", warm.Code, warm.Body.String())
	}

	req := probeRequest()
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		s.ServeHTTP(w, req)
	}
}

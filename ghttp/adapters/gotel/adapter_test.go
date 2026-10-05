package ghttpotel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sofiworker/gk/ghttp"
	"github.com/sofiworker/gk/gotel"
)

// ---- fake tracer ----

type fakeSC struct{ trace, span string }

func (s fakeSC) TraceID() string              { return s.trace }
func (s fakeSC) SpanID() string               { return s.span }
func (s fakeSC) IsSampled() bool              { return true }
func (s fakeSC) Serialize() map[string]string { return map[string]string{"trace": s.trace} }

type fakeSpan struct {
	mu       sync.Mutex
	name     string
	parent   string
	sc       fakeSC
	attrs    map[string]any
	status   gotel.StatusCode
	statDesc string
	errs     []error
	ended    bool
}

func (s *fakeSpan) Context() gotel.SpanContext { return s.sc }
func (s *fakeSpan) SetAttributes(kv ...gotel.KeyValue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range kv {
		s.attrs[a.Key] = a.Value
	}
}
func (s *fakeSpan) SetStatus(c gotel.StatusCode, d string) { s.status, s.statDesc = c, d }
func (s *fakeSpan) RecordError(err error, _ ...gotel.KeyValue) {
	s.errs = append(s.errs, err)
}
func (s *fakeSpan) AddEvent(string, ...gotel.KeyValue) {}
func (s *fakeSpan) End(...gotel.SpanEndOption)         { s.ended = true }

type spanKey struct{}
type parentKey struct{}

type fakeTracer struct {
	mu    sync.Mutex
	spans []*fakeSpan
}

func (t *fakeTracer) Start(ctx context.Context, name string, _ ...gotel.SpanStartOption) (context.Context, gotel.Span) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sp := &fakeSpan{name: name, attrs: map[string]any{}, sc: fakeSC{trace: "trace-1", span: "span-1"}}
	if p, ok := ctx.Value(parentKey{}).(string); ok {
		sp.parent = p
		sp.sc.trace = p
	}
	t.spans = append(t.spans, sp)
	return context.WithValue(ctx, spanKey{}, sp), sp
}

func (t *fakeTracer) Extract(ctx context.Context, c gotel.TextMapCarrier) (context.Context, gotel.SpanContext) {
	if v := c.Get("X-Fake-Trace"); v != "" {
		return context.WithValue(ctx, parentKey{}, v), fakeSC{trace: v}
	}
	return ctx, nil
}

func (t *fakeTracer) Inject(context.Context, gotel.TextMapCarrier, gotel.SpanContext) error {
	return nil
}

// ---- fake meter ----

type point struct {
	v     float64
	attrs map[string]any
}

type fakeInstr struct {
	mu  sync.Mutex
	pts []point
}

func (i *fakeInstr) rec(v float64, kv []gotel.KeyValue) {
	m := map[string]any{}
	for _, a := range kv {
		m[a.Key] = a.Value
	}
	i.mu.Lock()
	i.pts = append(i.pts, point{v, m})
	i.mu.Unlock()
}
func (i *fakeInstr) Add(_ context.Context, v float64, kv ...gotel.KeyValue) { i.rec(v, kv) }
func (i *fakeInstr) Increment(_ context.Context, kv ...gotel.KeyValue)      { i.rec(1, kv) }
func (i *fakeInstr) Record(_ context.Context, v float64, kv ...gotel.KeyValue) {
	i.rec(v, kv)
}

type fakeMeter struct {
	mu     sync.Mutex
	instrs map[string]*fakeInstr
}

func (m *fakeMeter) get(name string) *fakeInstr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.instrs == nil {
		m.instrs = map[string]*fakeInstr{}
	}
	if i, ok := m.instrs[name]; ok {
		return i
	}
	i := &fakeInstr{}
	m.instrs[name] = i
	return i
}
func (m *fakeMeter) Counter(n string, _ ...gotel.InstrumentOption) gotel.Counter { return m.get(n) }
func (m *fakeMeter) Histogram(n string, _ ...gotel.InstrumentOption) gotel.Histogram {
	return m.get(n)
}
func (m *fakeMeter) Gauge(n string, _ ...gotel.InstrumentOption) gotel.Gauge { return m.get(n) }

// ---- helpers ----

func newServer(t *testing.T, mws ...ghttp.Middleware) *ghttp.Server {
	t.Helper()
	s := ghttp.NewServer()
	s.Use(mws...)
	err := s.Register(
		ghttp.Get("/users/:id", func(ctx context.Context, _ ghttp.RequestOf[ghttp.NoDataType]) (string, error) {
			if _, ok := ctx.Value(spanKey{}).(*fakeSpan); !ok {
				return "", errors.New("no span in ctx")
			}
			return "ok", nil
		}),
		ghttp.Get("/forbidden", func(context.Context, ghttp.RequestOf[ghttp.NoDataType]) (string, error) {
			return "", ghttp.ErrForbidden
		}),
		ghttp.Get("/boom", func(context.Context, ghttp.RequestOf[ghttp.NoDataType]) (string, error) {
			return "", errors.New("boom")
		}),
		ghttp.Post("/echo", func(_ context.Context, _ ghttp.RequestOf[ghttp.NoDataType]) (string, error) {
			return "hello", nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(s *ghttp.Server, method, target string, body string, mut func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if mut != nil {
		mut(r)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// ---- tracing tests ----

func TestTracingSuccess(t *testing.T) {
	tr := &fakeTracer{}
	s := newServer(t, Tracing(tr, WithTraceIDHeader("X-Trace-Id")))
	w := do(s, "GET", "http://example.com:8080/users/42", "", func(r *http.Request) {
		r.Header.Set("X-Fake-Trace", "up-trace")
	})
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if len(tr.spans) != 1 {
		t.Fatalf("spans: %d", len(tr.spans))
	}
	sp := tr.spans[0]
	if sp.name != "GET /users/:id" || !sp.ended {
		t.Fatalf("span: %+v", sp)
	}
	want := map[string]any{
		AttrMethod: "GET", AttrRoute: "/users/:id", AttrURLPath: "/users/42",
		AttrStatusCode: int64(200), AttrServerAddr: "example.com",
	}
	for k, v := range want {
		if sp.attrs[k] != v {
			t.Errorf("attr %s = %v, want %v", k, sp.attrs[k], v)
		}
	}
	if sp.status != gotel.StatusCodeUnset || len(sp.errs) != 0 {
		t.Fatalf("unexpected error state: %+v", sp)
	}
	if sp.parent != "up-trace" {
		t.Fatalf("parent not extracted: %q", sp.parent)
	}
	if got := w.Header().Get("X-Trace-Id"); got != "up-trace" {
		t.Fatalf("trace header %q", got)
	}
}

func TestTracingErrors(t *testing.T) {
	tests := []struct {
		path      string
		status    int
		wantError bool
	}{
		{"/forbidden", 403, true},
		{"/boom", 500, true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			tr := &fakeTracer{}
			s := newServer(t, Tracing(tr))
			w := do(s, "GET", tt.path, "", nil)
			if w.Code != tt.status {
				t.Fatalf("status %d", w.Code)
			}
			sp := tr.spans[0]
			if sp.attrs[AttrStatusCode] != int64(tt.status) {
				t.Fatalf("attr: %v", sp.attrs[AttrStatusCode])
			}
			if (sp.status == gotel.StatusCodeError) != tt.wantError || (len(sp.errs) > 0) != tt.wantError {
				t.Fatalf("span: %+v", sp)
			}
		})
	}
}

func TestTracingUnmatched(t *testing.T) {
	tr := &fakeTracer{}
	s := newServer(t, Tracing(tr))
	w := do(s, "GET", "/no/such/42", "", nil)
	if w.Code != 404 {
		t.Fatalf("status %d", w.Code)
	}
	sp := tr.spans[0]
	if sp.name != "GET unmatched" {
		t.Fatalf("name %q", sp.name)
	}
	if _, ok := sp.attrs[AttrRoute]; ok {
		t.Fatal("route attr must be absent when unmatched")
	}
	if sp.attrs[AttrURLPath] != "/no/such/42" {
		t.Fatalf("path %v", sp.attrs[AttrURLPath])
	}
}

func TestTracingNilAndOptions(t *testing.T) {
	s := newServer(t, Tracing(nil))
	if w := do(s, "GET", "/boom", "", nil); w.Code != 500 {
		t.Fatalf("status %d", w.Code)
	}
	tr := &fakeTracer{}
	s = newServer(t, Tracing(tr, WithServerAddress("svc.local"), nil))
	do(s, "GET", "/users/1", "", nil)
	if tr.spans[0].attrs[AttrServerAddr] != "svc.local" {
		t.Fatalf("addr %v", tr.spans[0].attrs[AttrServerAddr])
	}
}

func TestHeaderCarrier(t *testing.T) {
	c := HeaderCarrier(http.Header{})
	c.Set("X-A", "1")
	if c.Get("x-a") != "1" {
		t.Fatal("get")
	}
	if k := c.Keys(); len(k) != 1 || k[0] != "X-A" {
		t.Fatalf("keys %v", k)
	}
}

// ---- metrics tests ----

func TestMetrics(t *testing.T) {
	m := &fakeMeter{}
	s := newServer(t, Metrics(m))
	w := do(s, "POST", "/echo", "abcd", nil)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}

	dur := m.get(MetricRequestDuration).pts
	if len(dur) != 1 || dur[0].v < 0 {
		t.Fatalf("duration: %+v", dur)
	}
	wantAttrs := map[string]any{AttrMethod: "POST", AttrRoute: "/echo", AttrStatusCode: int64(200)}
	if len(dur[0].attrs) != len(wantAttrs) {
		t.Fatalf("attrs not low cardinality: %v", dur[0].attrs)
	}
	for k, v := range wantAttrs {
		if dur[0].attrs[k] != v {
			t.Errorf("attr %s = %v want %v", k, dur[0].attrs[k], v)
		}
	}
	act := m.get(MetricActiveRequests).pts
	if len(act) != 2 || act[0].v != 1 || act[1].v != -1 {
		t.Fatalf("active: %+v", act)
	}
	if rb := m.get(MetricRequestBody).pts; len(rb) != 1 || rb[0].v != 4 {
		t.Fatalf("req body: %+v", rb)
	}
	if sb := m.get(MetricResponseBody).pts; len(sb) != 1 || sb[0].v <= 0 {
		t.Fatalf("resp body: %+v", sb)
	}
}

func TestMetricsErrorAndUnmatched(t *testing.T) {
	m := &fakeMeter{}
	s := newServer(t, Metrics(m, WithBodySizeMetrics(false), WithActiveRequests(false)))
	do(s, "GET", "/boom", "", nil)
	do(s, "GET", "/nope", "", nil)
	dur := m.get(MetricRequestDuration).pts
	if len(dur) != 2 {
		t.Fatalf("duration: %+v", dur)
	}
	if dur[0].attrs[AttrStatusCode] != int64(500) || dur[0].attrs[AttrRoute] != "/boom" {
		t.Fatalf("attrs: %v", dur[0].attrs)
	}
	if _, ok := dur[1].attrs[AttrRoute]; ok || dur[1].attrs[AttrStatusCode] != int64(404) {
		t.Fatalf("unmatched attrs: %v", dur[1].attrs)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.instrs) != 1 {
		t.Fatalf("only duration expected, got %d instruments", len(m.instrs))
	}
}

func TestMetricsNilMeter(t *testing.T) {
	s := newServer(t, Metrics(nil))
	if w := do(s, "POST", "/echo", "", nil); w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestTracingAndMetricsTogether(t *testing.T) {
	tr, m := &fakeTracer{}, &fakeMeter{}
	s := newServer(t, Tracing(tr), Metrics(m))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); do(s, "GET", "/users/1", "", nil) }()
	}
	wg.Wait()
	if len(m.get(MetricRequestDuration).pts) != 8 {
		t.Fatal("expected 8 duration points")
	}
}

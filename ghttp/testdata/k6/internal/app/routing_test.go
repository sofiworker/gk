package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/sofiworker/gk/ghttp"
)

func request(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("X-Request-ID", "test-request-id")
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestHealthAndReady(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	for _, tc := range []struct {
		path string
		body string
	}{
		{path: "/health", body: `{"status":"ok"}`},
		{path: "/ready", body: `{"status":"ready"}`},
	} {
		recorder := request(t, handler, http.MethodGet, tc.path)
		if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != tc.body {
			t.Fatalf("GET %s = %d %q, want 200 %q", tc.path, recorder.Code, recorder.Body.String(), tc.body)
		}
		if got := recorder.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("GET %s Content-Type = %q", tc.path, got)
		}
		if got := recorder.Header().Get("X-Request-ID"); got != "test-request-id" {
			t.Fatalf("GET %s X-Request-ID = %q", tc.path, got)
		}
	}

	recorder := request(t, handler, http.MethodHead, "/health")
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("HEAD /health = %d %q, want 200 empty body", recorder.Code, recorder.Body.String())
	}
}

func TestHealthGeneratesRequestID(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if got := recorder.Header().Get("X-Request-ID"); !strings.HasPrefix(got, "k6-") || len(got) != 19 {
		t.Fatalf("generated X-Request-ID = %q", got)
	}
}

func TestRoutingMatrix(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	tests := []struct {
		path string
		want string
	}{
		{path: "/routes/static", want: `{"route":"static"}`},
		{path: "/routes/users/new", want: `{"id":"new","route":"static"}`},
		{path: "/routes/users/42", want: `{"id":"42"}`},
		{path: "/routes/pairs/left/right", want: `{"left":"left","right":"right"}`},
		{path: "/routes/files/css/app.css", want: `{"path":"css/app.css"}`},
		{path: "/routes/groups/v1/items/7", want: `{"id":"7"}`},
		{path: "/routes/unicode/%E4%B8%AD%E6%96%87", want: `{"value":"中文"}`},
	}
	for _, tc := range tests {
		recorder := request(t, handler, http.MethodGet, tc.path)
		if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != tc.want {
			t.Errorf("GET %s = %d %q, want 200 %q", tc.path, recorder.Code, recorder.Body.String(), tc.want)
		}
	}
}

func TestRoutingMethodsAndErrorsCarryRequestID(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		recorder := request(t, handler, method, "/routes/method")
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s /routes/method = %d, want 204", method, recorder.Code)
		}
	}
	recorder := request(t, handler, http.MethodPut, "/routes/method")
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /routes/method = %d, want 405", recorder.Code)
	}
	allow := recorder.Header().Get("Allow")
	methods := strings.Split(allow, ", ")
	sort.Strings(methods)
	if !slices.Equal(methods, []string{http.MethodDelete, http.MethodPost}) {
		t.Fatalf("Allow = %q, want exact method set DELETE, POST", allow)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("405 response missing X-Request-ID")
	}
	recorder = request(t, handler, http.MethodGet, "/missing")
	if recorder.Code != http.StatusNotFound || recorder.Header().Get("X-Request-ID") == "" {
		t.Fatalf("GET /missing = %d request-id %q", recorder.Code, recorder.Header().Get("X-Request-ID"))
	}
}

func TestTrailingSlashIsNormalized(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	recorder := request(t, handler, http.MethodGet, "/health/")
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("GET /health/ = %d %q, want normalized health response", recorder.Code, recorder.Body.String())
	}
}

func TestRawPathPreservation(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	for _, tc := range []struct {
		target      string
		pathValue   string
		rawPath     string
		escapedPath string
		rawQuery    string
	}{
		{target: "/routes/raw-path/a%2Fb?x=a%2Fb", pathValue: "a/b", rawPath: "/routes/raw-path/a%2Fb", escapedPath: "/routes/raw-path/a%2Fb", rawQuery: "x=a%2Fb"},
		{target: "/routes/raw-path/a%252Fb?x=a%252Fb", pathValue: "a%2Fb", rawPath: "", escapedPath: "/routes/raw-path/a%252Fb", rawQuery: "x=a%252Fb"},
	} {
		recorder := request(t, handler, http.MethodGet, tc.target)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %q", tc.target, recorder.Code, recorder.Body.String())
		}
		var got struct {
			Value       string `json:"value"`
			PathValue   string `json:"path_value"`
			Path        string `json:"path"`
			RawPath     string `json:"raw_path"`
			EscapedPath string `json:"escaped_path"`
			RawQuery    string `json:"raw_query"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Value != tc.pathValue || got.RawPath != tc.rawPath || got.EscapedPath != tc.escapedPath || got.RawQuery != tc.rawQuery {
			t.Fatalf("GET %s response = %+v", tc.target, got)
		}
		if got.PathValue != "" {
			t.Fatalf("GET %s Request.PathValue(value) = %q, want current native empty value", tc.target, got.PathValue)
		}
	}
}

func TestMetricsAndReset(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	request(t, handler, http.MethodGet, "/health")
	recorder := request(t, handler, http.MethodGet, "/__test/metrics")
	var snapshot MetricsSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	// 指标端点自排除,因此快照只反映先前 /health 请求。
	// The metrics endpoint is self-excluded, so the snapshot reflects only the
	// preceding /health request.
	if snapshot.TotalRequests < 1 || snapshot.Statuses["200"] == 0 {
		t.Fatalf("metrics = %+v, want reflected requests", snapshot)
	}
	recorder = request(t, handler, http.MethodPost, "/__test/reset")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("reset status = %d, want 204", recorder.Code)
	}
	recorder = request(t, handler, http.MethodGet, "/__test/metrics")
	snapshot = MetricsSnapshot{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	// 指标端点自排除:reset 后快照全零。
	// The metrics endpoint is self-excluded: after reset the snapshot is all zero.
	if snapshot.TotalRequests != 0 {
		t.Fatalf("total requests after reset and metrics request = %d, want 0", snapshot.TotalRequests)
	}
	if snapshot.ActiveRequests != 0 || snapshot.PeakRequests != 0 || snapshot.RequestBytes != 0 || snapshot.ResponseBytes != 0 || snapshot.SSEConnections != 0 || snapshot.WSConnections != 0 || len(snapshot.Statuses) != 0 {
		t.Fatalf("application counters after reset = %+v, want all zero (metrics self-excluded)", snapshot)
	}
}

func TestMetricsResponseBytesUseTransmittedBody(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)

	request(t, handler, http.MethodPost, "/__test/reset")
	get := request(t, handler, http.MethodGet, "/health")
	getMetrics := request(t, handler, http.MethodGet, "/__test/metrics")
	var getSnapshot MetricsSnapshot
	if err := json.Unmarshal(getMetrics.Body.Bytes(), &getSnapshot); err != nil {
		t.Fatal(err)
	}
	if getSnapshot.ResponseBytes != uint64(get.Body.Len()) || getSnapshot.ResponseBytes == 0 {
		t.Fatalf("GET response bytes = %d, body bytes = %d", getSnapshot.ResponseBytes, get.Body.Len())
	}

	request(t, handler, http.MethodPost, "/__test/reset")
	head := request(t, handler, http.MethodHead, "/health")
	headMetrics := request(t, handler, http.MethodGet, "/__test/metrics")
	var headSnapshot MetricsSnapshot
	if err := json.Unmarshal(headMetrics.Body.Bytes(), &headSnapshot); err != nil {
		t.Fatal(err)
	}
	if head.Body.Len() != 0 || headSnapshot.ResponseBytes != 0 {
		t.Fatalf("HEAD body bytes = %d metrics response bytes = %d, want both 0", head.Body.Len(), headSnapshot.ResponseBytes)
	}
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate route registration did not panic")
		}
	}()
	registerDuplicateRouteForTest()
}

func registerDuplicateRouteForTest() {
	server := ghttp.New()
	mustRaw(server.RawHandle(http.MethodGet, "/same", rawAdapter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))))
	mustRaw(server.RawHandle(http.MethodGet, "/same", rawAdapter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))))
}

func TestObserveRequestsImplicitStatus(t *testing.T) {
	metrics := NewRuntimeMetrics()
	server := ghttp.New()
	server.Use(observeRequests(metrics))
	mustRaw(server.RawHandle(http.MethodGet, "/implicit", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
		w.WriteHeader(http.StatusTeapot)
	}))))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/implicit", nil))
	got := metrics.Snapshot()
	if got.Statuses["200"] != 1 || got.Statuses["418"] != 0 {
		t.Fatalf("statuses = %#v, want only implicit 200", got.Statuses)
	}
}

func TestObserveRequestsBodylessStatusBytes(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		metrics := NewRuntimeMetrics()
		server := ghttp.New()
		server.Use(observeRequests(metrics))
		mustRaw(server.RawHandle(http.MethodGet, "/bodyless", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("not transmitted"))
		}))))
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/bodyless", nil))
		if got := metrics.Snapshot().ResponseBytes; got != 0 {
			t.Errorf("status %d response bytes = %d, want 0", status, got)
		}
	}
}

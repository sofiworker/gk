package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestMiddlewareOrderAndHeaders(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	sink := newTraceSink()
	recorder := serveRequest(handler, withTraceSink(newTestRequest(http.MethodGet, "/middleware/order"), sink))
	if recorder.Code != http.StatusOK {
		t.Fatalf("middleware route = %d %s", recorder.Code, recorder.Body.String())
	}
	wantSteps := []string{"global-before", "group-before", "route-before", "handler", "route-after", "group-after", "global-after"}
	if got := sink.Snapshot(); !equalStrings(got, wantSteps) {
		t.Fatalf("trace steps = %#v, want %#v", got, wantSteps)
	}
	result := recorder.Result()
	vary := strings.Join(result.Header.Values("Vary"), ",")
	for _, want := range []string{"X-Global", "X-Group", "X-Route"} {
		if !strings.Contains(vary, want) {
			t.Fatalf("Vary = %q, missing %q", vary, want)
		}
	}
	if result.Header.Get("X-Trace-Owner") != "route" {
		t.Fatalf("X-Trace-Owner = %q, want route override", result.Header.Get("X-Trace-Owner"))
	}
}

func TestMiddlewareAuthShortCircuit(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	recorder := request(t, handler, http.MethodGet, "/auth/admin")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("auth short circuit status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Auth-Handler"); got != "" {
		t.Fatalf("short-circuited handler executed: X-Auth-Handler=%q", got)
	}
}

func TestMiddlewareRequestIDOnPanicAndRoutingErrors(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	for _, tc := range []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodGet, path: "/fault/panic", status: 500},
		{method: http.MethodGet, path: "/not-found", status: 404},
		{method: http.MethodPost, path: "/health", status: 405},
	} {
		recorder := request(t, handler, tc.method, tc.path)
		if recorder.Code != tc.status || recorder.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s %s = %d request-id=%q", tc.method, tc.path, recorder.Code, recorder.Header().Get("X-Request-ID"))
		}
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

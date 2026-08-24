package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestErrorClassificationAndSafety(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	tests := []struct {
		path        string
		status      int
		message     string
		contentType string
	}{
		{path: "/errors/ordinary", status: 500, message: "Internal Server Error", contentType: "application/json"},
		{path: "/errors/wrapped", status: 409, message: "Conflict", contentType: "application/json"},
		{path: "/errors/joined", status: 503, message: "Service Unavailable", contentType: "application/json"},
		{path: "/errors/gerr", status: 503, message: "Service Unavailable", contentType: "application/json"},
		{path: "/errors/validation", status: 422, message: "Unprocessable Entity", contentType: "application/json"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			recorder := request(t, handler, http.MethodGet, tc.path)
			assertJSONError(t, recorder.Code, recorder.Header().Get("Content-Type"), recorder.Header().Get("X-Request-ID"), recorder.Body.Bytes(), tc.status, tc.message)
			assertNoInternalLeak(t, recorder.Body.String())
		})
	}
}

func TestErrorAdapterRunsOnceOnMainRoute(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	counter := newRequestCounter()
	req := withRequestCounter(newTestRequest(http.MethodGet, "/errors/wrapped"), counter)
	recorder := serveRequest(handler, req)
	if recorder.Code != http.StatusConflict || counter.Value() != 1 {
		t.Fatalf("error adapter status/count = %d/%d, want 409/1", recorder.Code, counter.Value())
	}
}

func TestFaultControlledEndpoints(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	tests := []struct {
		path   string
		header string
		status int
	}{
		{path: "/fault/random?rate=0.5", header: "pass", status: 200},
		{path: "/fault/random?rate=0.5", header: "fail", status: 503},
		{path: "/fault/limited", header: "reject", status: 429},
		{path: "/fault/unavailable", status: 503},
	}
	for _, tc := range tests {
		req := newTestRequest(http.MethodGet, tc.path)
		if tc.header != "" {
			req.Header.Set("X-Fault-Result", tc.header)
		}
		recorder := serveRequest(handler, req)
		if recorder.Code != tc.status || recorder.Header().Get("X-Request-ID") == "" {
			t.Fatalf("GET %s = %d request-id=%q body=%s", tc.path, recorder.Code, recorder.Header().Get("X-Request-ID"), recorder.Body.String())
		}
		if tc.status >= 500 {
			assertNoInternalLeak(t, recorder.Body.String())
		}
	}
}

func TestFaultRandomSequenceRepeatsAfterReset(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	sequence := func() []int {
		statuses := make([]int, 8)
		for index := range statuses {
			statuses[index] = request(t, handler, http.MethodGet, "/fault/random?rate=0.5").Code
		}
		return statuses
	}
	first := sequence()
	if request(t, handler, http.MethodPost, "/fault/reset").Code != http.StatusNoContent {
		t.Fatal("fault reset failed")
	}
	second := sequence()
	if !equalInts(first, second) || !containsInt(first, http.StatusOK) || !containsInt(first, http.StatusServiceUnavailable) {
		t.Fatalf("random sequences = %#v %#v, want repeatable mixed outcomes", first, second)
	}
}

func TestFaultLimitedUsesConcurrentGate(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := withFaultStarted(newTestRequest(http.MethodGet, "/fault/limited?hold=1").WithContext(ctx), started)
		firstDone <- serveRequest(handler, req)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("limited holder did not acquire gate")
	}
	second := request(t, handler, http.MethodGet, "/fault/limited")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second limited request = %d, want 429", second.Code)
	}
	cancel()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("limited holder did not exit after cancel")
	}
}

func TestFaultDelayIsContextAwareAndCapped(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	started := time.Now()
	recorder := request(t, handler, http.MethodGet, "/fault/delay?ms=9999")
	elapsed := time.Since(started)
	if recorder.Code != http.StatusOK || elapsed > time.Second {
		t.Fatalf("capped delay = status %d elapsed %s body=%s", recorder.Code, elapsed, recorder.Body.String())
	}
}

func TestPanicRecoveryIsSafeAndMetricsRecord500(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	request(t, handler, http.MethodPost, "/__test/reset")
	recorder := request(t, handler, http.MethodGet, "/fault/panic")
	if recorder.Code != http.StatusInternalServerError || recorder.Header().Get("X-Request-ID") == "" {
		t.Fatalf("panic response = %d request-id=%q body=%s", recorder.Code, recorder.Header().Get("X-Request-ID"), recorder.Body.String())
	}
	assertNoInternalLeak(t, recorder.Body.String())
	metricsRecorder := request(t, handler, http.MethodGet, "/__test/metrics")
	var snapshot MetricsSnapshot
	if err := json.Unmarshal(metricsRecorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Statuses["500"] != 1 {
		t.Fatalf("panic metrics = %#v, want one 500", snapshot.Statuses)
	}
}

func TestTimeoutUsesFrameworkGatewayTimeout(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	started := time.Now()
	lateDone := make(chan struct{})
	req := withFaultLateDone(newTestRequest(http.MethodGet, "/fault/timeout"), lateDone)
	recorder := serveRequest(handler, req)
	// ghttp.Timeout 是协作式超时,超时未提交响应时补写 503。
	// ghttp.Timeout is cooperative and fills in 503 when the deadline passes
	// without a committed response.
	if recorder.Code != http.StatusServiceUnavailable || time.Since(started) > time.Second {
		t.Fatalf("timeout response = %d elapsed=%s body=%s", recorder.Code, time.Since(started), recorder.Body.String())
	}
	assertNoInternalLeak(t, recorder.Body.String())
	select {
	case <-lateDone:
	case <-time.After(time.Second):
		t.Fatal("timeout handler late write did not finish")
	}
	if strings.Contains(recorder.Body.String(), "late-success") {
		t.Fatalf("timeout exposed late success: %s", recorder.Body.String())
	}
	assertOnlyMetricsRequestActive(t, handler)
}

func TestCancelIsObservedWithoutSuccessWrite(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := withFaultStarted(newTestRequest(http.MethodGet, "/fault/cancel?wait_ms=500").WithContext(ctx), started)
		done <- serveRequest(handler, req)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("cancel handler did not start")
	}
	cancel()
	var recorder *httptest.ResponseRecorder
	select {
	case recorder = <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel handler did not exit")
	}
	if recorder.Code != http.StatusRequestTimeout || recorder.Header().Get("X-Cancel-Observed") != "true" {
		t.Fatalf("cancel response = %d observed=%q body=%s", recorder.Code, recorder.Header().Get("X-Cancel-Observed"), recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "success") {
		t.Fatalf("canceled handler wrote success: %s", recorder.Body.String())
	}
	assertOnlyMetricsRequestActive(t, handler)
}

func assertOnlyMetricsRequestActive(t *testing.T, handler http.Handler) {
	t.Helper()
	recorder := request(t, handler, http.MethodGet, "/__test/metrics")
	var snapshot MetricsSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	// 指标端点自排除:快照不包含本次指标请求自身。
	// The metrics endpoint is self-excluded: the snapshot does not include the
	// metrics request itself.
	if snapshot.ActiveRequests != 0 {
		t.Fatalf("active requests = %d, want metrics request self-excluded", snapshot.ActiveRequests)
	}
}

func equalInts(left, right []int) bool {
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

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestErrorControlledStatuses(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	for _, status := range []int{400, 401, 403, 404, 405, 409, 412, 413, 415, 422, 429, 500, 503, 504} {
		recorder := request(t, handler, http.MethodGet, "/errors/status/"+httpStatusString(status))
		assertJSONError(t, recorder.Code, recorder.Header().Get("Content-Type"), recorder.Header().Get("X-Request-ID"), recorder.Body.Bytes(), status, http.StatusText(status))
		assertNoInternalLeak(t, recorder.Body.String())
	}
}

func TestProblemDetails(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	recorder := request(t, handler, http.MethodGet, "/problem/not-found")
	if recorder.Code != http.StatusNotFound || recorder.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("problem response = %d %q %s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	var problem map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"type", "title", "status", "detail", "instance"} {
		if _, ok := problem[key]; !ok {
			t.Fatalf("problem missing %q: %#v", key, problem)
		}
	}
	if problem["instance"] != "/problem/not-found" || recorder.Header().Get("X-Request-ID") == "" {
		t.Fatalf("problem instance/request ID = %#v %q", problem["instance"], recorder.Header().Get("X-Request-ID"))
	}
	assertNoInternalLeak(t, recorder.Body.String())
}

func TestAuthMatrix(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	tests := []struct {
		name   string
		path   string
		token  string
		status int
	}{
		{name: "missing", path: "/auth/user", status: 401},
		{name: "invalid", path: "/auth/user", token: "invalid", status: 401},
		{name: "user", path: "/auth/user", token: "user:" + testSecret, status: 200},
		{name: "user forbidden", path: "/auth/admin", token: "user:" + testSecret, status: 403},
		{name: "admin", path: "/auth/admin", token: "admin:" + testSecret, status: 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := newTestRequest(http.MethodGet, tc.path)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			recorder := serveRequest(handler, req)
			if recorder.Code != tc.status || recorder.Header().Get("X-Request-ID") == "" {
				t.Fatalf("auth response = %d request-id=%q body=%s", recorder.Code, recorder.Header().Get("X-Request-ID"), recorder.Body.String())
			}
			if tc.status >= 400 {
				assertNoInternalLeak(t, recorder.Body.String())
			}
		})
	}
}

func assertJSONError(t *testing.T, gotStatus int, contentType, requestID string, body []byte, wantStatus int, wantMessage string) {
	t.Helper()
	if gotStatus != wantStatus || contentType != "application/json" || requestID == "" {
		t.Fatalf("error response = status %d content-type %q request-id %q body %s", gotStatus, contentType, requestID, body)
	}
	var got struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != wantStatus || got.Message != wantMessage {
		t.Fatalf("error body = %+v, want code=%d message=%q", got, wantStatus, wantMessage)
	}
}

func assertNoInternalLeak(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{testSecret, "stack", "ordinary-internal", "conflict-sentinel", "unavailable-sentinel"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
}

func httpStatusString(status int) string {
	return strconv.Itoa(status)
}

func newTestRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("X-Request-ID", "test-request-id")
	return request
}

func serveRequest(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

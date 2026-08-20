package ghttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientBasicGet(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	type helloResp struct {
		Body struct {
			Message string `json:"message"`
		}
	}
	app.MustMount(Handle(Get("/hello"), NoInput(), JSONOutput[helloResp](), func(ctx context.Context, _ EmptyInput) (helloResp, error) {
		return helloResp{Body: struct {
			Message string `json:"message"`
		}{Message: "Hello"}}, nil
	}))

	ts := httptest.NewServer(app)
	defer ts.Close()

	client := NewClient()
	resp, err := client.R().
		SetHeader("Accept", "application/json").
		Get(ts.URL + "/hello")

	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("expected success, got %d", resp.StatusCode)
	}
}

func TestClientBaseURL(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	type pongResp struct {
		Body struct{ Pong string }
	}
	app.MustMount(Handle(Get("/ping"), NoInput(), JSONOutput[pongResp](), func(ctx context.Context, _ EmptyInput) (pongResp, error) {
		return pongResp{Body: struct{ Pong string }{Pong: "ok"}}, nil
	}))

	ts := httptest.NewServer(app)
	defer ts.Close()

	client := NewClient(WithBaseURL(ts.URL))
	resp, err := client.R().Get("/ping")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("expected success, got %d", resp.StatusCode)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestClientWithHTTPClientUsesProvidedClient(t *testing.T) {
	called := false
	client := NewClient(WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Request:    r,
			}, nil
		}),
	}))

	resp, err := client.R().Get("http://example.test/ok")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !called {
		t.Fatal("custom http client transport was not called")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestClientWithTransportUsesProvidedRoundTripper(t *testing.T) {
	called := false
	client := NewClient(WithTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Status:     "202 Accepted",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`accepted`)),
			Request:    r,
		}, nil
	})))

	resp, err := client.R().Get("http://example.test/accepted")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !called {
		t.Fatal("custom transport was not called")
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
}

func TestClientResponseRawBodyStreamsWithoutPreloading(t *testing.T) {
	closed := false
	client := NewClient(WithTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body: &trackingReadCloser{
				Reader: strings.NewReader("streamed response"),
				closed: &closed,
			},
			Request: r,
		}, nil
	})))

	resp, err := client.R().SetStreamResponse(true).Get("http://example.test/stream")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(resp.Body) != 0 {
		t.Fatalf("Body len = %d, want 0 for streamed response", len(resp.Body))
	}
	if closed {
		t.Fatal("response body was closed before caller consumed RawBody")
	}

	data, err := io.ReadAll(resp.RawBody())
	if err != nil {
		t.Fatalf("ReadAll RawBody failed: %v", err)
	}
	if string(data) != "streamed response" {
		t.Fatalf("RawBody = %q, want streamed response", string(data))
	}
	if err := resp.RawBody().Close(); err != nil {
		t.Fatalf("RawBody close failed: %v", err)
	}
	if !closed {
		t.Fatal("RawBody close did not close underlying response body")
	}
}

func TestClientResponseRawBodyAvailableAfterBufferedRead(t *testing.T) {
	client := NewClient(WithTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("buffered response")),
			Request:    r,
		}, nil
	})))

	resp, err := client.R().Get("http://example.test/buffered")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	data, err := io.ReadAll(resp.RawBody())
	if err != nil {
		t.Fatalf("ReadAll RawBody failed: %v", err)
	}
	if string(data) != "buffered response" {
		t.Fatalf("RawBody = %q, want buffered response", string(data))
	}
}

type trackingReadCloser struct {
	io.Reader
	closed *bool
}

func (r *trackingReadCloser) Close() error {
	*r.closed = true
	return nil
}

type clientGreetReq struct {
	Body struct {
		Name string `json:"name"`
	}
}
type clientGreetResp struct {
	Body struct {
		Message string `json:"message"`
	}
}

func TestClientGenericEndpoint(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/greet"), JSONBody[struct {
		Name string `json:"name"`
	}](), JSONOutput[clientGreetResp](), func(ctx context.Context, req struct {
		Name string `json:"name"`
	}) (clientGreetResp, error) {
		return clientGreetResp{Body: struct {
			Message string `json:"message"`
		}{Message: "Hello, " + req.Name}}, nil
	}))

	ts := httptest.NewServer(app)
	defer ts.Close()

	client := NewClient(WithBaseURL(ts.URL))

	// The Do function sends JSON to the server which gets wrapped in envelope.
	// The Do function then unwraps the envelope. This is a test of that flow.
	resp, err := Do[clientGreetReq, clientGreetResp](context.Background(), client, http.MethodPost, "/greet", &clientGreetReq{
		Body: struct {
			Name string `json:"name"`
		}{Name: "Alice"},
	})
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	if resp.Body.Message != "Hello, Alice" {
		t.Fatalf("expected 'Hello, Alice', got %s", resp.Body.Message)
	}
}

func TestClientRetryOnServerError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	client.SetRetryCount(3).SetRetryWaitTime(5 * time.Millisecond)

	resp, err := client.R().Get("/x")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
	if !resp.IsSuccess() {
		t.Fatalf("status = %d, want success", resp.StatusCode)
	}
}

func TestClientRetryConditionStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`"ok"`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	client.SetRetryCount(2).
		SetRetryWaitTime(5 * time.Millisecond).
		SetRetryConditions(func(resp *Response, err error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		})

	resp, err := client.R().Get("/x")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
	if !resp.IsSuccess() {
		t.Fatalf("status = %d, want success", resp.StatusCode)
	}
}

func TestClientRetryStopsOnSuccess(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	client.SetRetryCount(3).SetRetryWaitTime(5 * time.Millisecond)
	if _, err := client.R().Get("/x"); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestClientHooksBeforeAndAfter(t *testing.T) {
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hook") != "1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	client.OnBeforeRequest(func(r *Request) error {
		order = append(order, "client-before")
		r.SetHeader("X-Hook", "1")
		return nil
	})
	client.OnAfterResponse(func(r *Response) error {
		order = append(order, "client-after")
		return nil
	})

	resp, err := client.R().
		OnBeforeRequest(func(r *Request) error {
			order = append(order, "req-before")
			return nil
		}).
		OnAfterResponse(func(r *Response) error {
			order = append(order, "req-after")
			return nil
		}).
		Get("/x")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("status = %d, want success", resp.StatusCode)
	}
	want := []string{"client-before", "req-before", "client-after", "req-after"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
}

func TestClientErrorBinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"message":"bad request"}`))
	}))
	defer server.Close()

	type apiError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	var target apiError
	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.R().SetError(&target).Get("/err")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !resp.IsError() {
		t.Fatalf("status = %d, want error", resp.StatusCode)
	}
	if target.Message != "bad request" {
		t.Fatalf("error message = %q, want bad request", target.Message)
	}
	bound, ok := resp.Error().(*apiError)
	if !ok || bound.Code != 400 {
		t.Fatalf("resp.Error() = %#v, want bound apiError", resp.Error())
	}
}

func TestClientBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "alice" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.R().SetBasicAuth("alice", "secret").Get("/x")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("status = %d, want success", resp.StatusCode)
	}
}

func TestClientSetOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("file-content"))
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "out.txt")
	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.R().SetOutput(output).Get("/download")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(resp.Body) != "file-content" {
		t.Fatalf("body = %q, want file-content", resp.Body)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "file-content" {
		t.Fatalf("output file = %q, want file-content", data)
	}
}

func TestClientQueryStringAndValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.RawQuery))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.R().
		SetQueryParam("a", "1").
		SetQueryParams(map[string]string{"b": "2"}).
		SetQueryParamsFromValues(url.Values{"c": {"3", "4"}}).
		SetQueryString("d=5&e=6").
		Get("/q")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	query := string(resp.Body)
	for _, want := range []string{"a=1", "b=2", "c=3", "c=4", "d=5", "e=6"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query = %q, missing %s", query, want)
		}
	}
}

func TestClientPerRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	_, err := client.R().SetTimeout(10 * time.Millisecond).Get("/slow")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestClientClientCookies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("sess"); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	client.SetCookie(&http.Cookie{Name: "sess", Value: "abc"})
	resp, err := client.R().Get("/x")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("status = %d, want success", resp.StatusCode)
	}
}

func TestClientResponseAccessors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1"})
		_, _ = w.Write([]byte(`{"name":"alice"}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.R().Get("/x")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if resp.Time() < 0 {
		t.Fatalf("Time() = %s, want >= 0", resp.Time())
	}
	if resp.ReceivedAt().IsZero() {
		t.Fatal("ReceivedAt() is zero")
	}
	if resp.Size() != int64(len(`{"name":"alice"}`)) {
		t.Fatalf("Size() = %d", resp.Size())
	}
	if len(resp.Cookies()) != 1 || resp.Cookies()[0].Name != "sid" {
		t.Fatalf("Cookies() = %#v", resp.Cookies())
	}
	var out struct {
		Name string `json:"name"`
	}
	if err := resp.Unmarshal(&out); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if out.Name != "alice" {
		t.Fatalf("name = %q, want alice", out.Name)
	}
}

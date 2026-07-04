package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientBasicGet(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	type helloResp struct {
		Body struct {
			Message string `json:"message"`
		}
	}
	Route[struct{ Body struct{} }, helloResp](app).GET("/hello").To(func(ctx context.Context, req struct{ Body struct{} }) (helloResp, error) {
		return helloResp{Body: struct {
			Message string `json:"message"`
		}{Message: "Hello"}}, nil
	})

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
	Route[struct{ Body struct{} }, pongResp](app).GET("/ping").To(func(ctx context.Context, req struct{ Body struct{} }) (pongResp, error) {
		return pongResp{Body: struct{ Pong string }{Pong: "ok"}}, nil
	})

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
	Route[clientGreetReq, clientGreetResp](app).POST("/greet").To(func(ctx context.Context, req clientGreetReq) (clientGreetResp, error) {
		return clientGreetResp{Body: struct {
			Message string `json:"message"`
		}{Message: "Hello, " + req.Body.Name}}, nil
	})

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

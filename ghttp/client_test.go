package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
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

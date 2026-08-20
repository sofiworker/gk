//go:build go1.27

package ghttp

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestGo127ClientGenericMethods(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/greet"), JSONBody[struct{ Name string `json:"name"` }](), JSONOutput[clientGreetResp](), func(ctx context.Context, req struct {
		Name string `json:"name"`
	}) (clientGreetResp, error) {
		return clientGreetResp{Body: struct {
			Message string `json:"message"`
		}{Message: "Hello, " + req.Name}}, nil
	}))
	app.MustMount(Handle(Get("/ping"), NoInput(), JSONOutput[clientGreetResp](), func(ctx context.Context, _ EmptyInput) (clientGreetResp, error) {
		return clientGreetResp{Body: struct {
			Message string `json:"message"`
		}{Message: "pong"}}, nil
	}))

	ts := httptest.NewServer(app)
	defer ts.Close()
	client := NewClient(WithBaseURL(ts.URL))

	resp, err := client.Post[clientGreetReq, clientGreetResp](context.Background(), "/greet", &clientGreetReq{
		Body: struct {
			Name string `json:"name"`
		}{Name: "Bob"},
	})
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}
	if resp.Body.Message != "Hello, Bob" {
		t.Fatalf("message = %q, want Hello, Bob", resp.Body.Message)
	}

	ping, err := client.Get[struct{}, clientGreetResp](context.Background(), "/ping")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if ping.Body.Message != "pong" {
		t.Fatalf("message = %q, want pong", ping.Body.Message)
	}
}

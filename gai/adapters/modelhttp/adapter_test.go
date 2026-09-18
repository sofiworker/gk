package modelhttp

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/model"
	"github.com/sofiworker/gk/gai/transport/httptransport"
)

type testProtocol struct {
	called   bool
	received httptransport.Response
}

func (p *testProtocol) Encode(context.Context, model.Request) (httptransport.Request, error) {
	p.called = true
	return httptransport.Request{Method: "POST", URL: "https://example.test"}, nil
}
func (p *testProtocol) Decode(_ context.Context, r httptransport.Response) (model.Response, error) {
	p.received = r
	return model.Response{FinishReason: model.FinishStop}, nil
}

type transportFunc func(context.Context, httptransport.Request) (httptransport.Response, error)

func (f transportFunc) Do(ctx context.Context, r httptransport.Request) (httptransport.Response, error) {
	return f(ctx, r)
}
func TestGenerateBoundaries(t *testing.T) {
	p := &testProtocol{}
	sentinel := errors.New("transport failed")
	calls := 0
	c, err := New(p, transportFunc(func(context.Context, httptransport.Request) (httptransport.Response, error) {
		calls++
		if calls == 2 {
			return httptransport.Response{}, sentinel
		}
		return httptransport.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"3"}}, Body: []byte("limited")}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	r := model.Request{Model: core.ModelSelection{ID: "logical"}, Parameters: model.Parameters{MaxOutputTokens: &n}}
	if _, err = c.Generate(context.Background(), r); !errors.Is(err, model.ErrParameters) || p.called {
		t.Fatalf("invalid parameters reached protocol: %v", err)
	}
	r.Parameters = model.Parameters{}
	if _, err = c.Generate(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if p.received.StatusCode != 429 || p.received.Header.Get("Retry-After") != "3" || string(p.received.Body) != "limited" {
		t.Fatal("HTTP response was not preserved")
	}
	if _, err = c.Generate(context.Background(), r); !errors.Is(err, sentinel) || calls != 2 {
		t.Fatalf("transport error or retry: %v, calls %d", err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.Generate(ctx, r); !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatal("cancelled call reached transport")
	}
	if _, err = New(nil, c.transport); !errors.Is(err, ErrConfig) {
		t.Fatal(err)
	}
}

package ghttp

import (
	"context"
	"errors"
	"testing"
)

type testInput struct {
	Params `json:"-"`

	Body struct {
		Name string `json:"name"`
	}
}

type testOutput struct {
	Status int `default:"200"`
	Body   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
}

func testHandler(_ context.Context, request testInput) (testOutput, error) {
	return testOutput{Body: struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{ID: request.Path("id"), Name: request.Body.Name}}, nil
}

func middlewareSetHeader(name, value string) Middleware {
	return func(c *Ctx) {
		c.W.Header().Set(name, value)
		c.Next()
	}
}

func assertPanicsIs(t *testing.T, want error, fn func()) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("expected panic matching %v", want)
		}
		err, ok := got.(error)
		if !ok {
			t.Fatalf("panic = %v, want error matching %v", got, want)
		}
		if !errors.Is(err, want) {
			t.Fatalf("panic error = %v, want %v", err, want)
		}
	}()
	fn()
}

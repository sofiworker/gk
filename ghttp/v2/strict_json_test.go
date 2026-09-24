package v2

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStrictJSONInput(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	for _, tc := range []struct {
		body  string
		valid bool
	}{{`{"name":"ok"}`, true}, {`{"name":"ok","extra":1}`, false}, {`{} {}`, false}, {`{`, false}, {`null`, true}} {
		t.Run(tc.body, func(t *testing.T) {
			called := false
			route := Post("/", func(context.Context, *input) (string, error) { called = true; return "ok", nil }, WithInput(StrictJSONInput[*input]()))
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			err := route.Serve(context.Background(), &Request{Request: req}, &Response{ResponseWriter: httptest.NewRecorder()})
			if (err == nil) != tc.valid || called != tc.valid {
				t.Fatalf("error %v called %v", err, called)
			}
		})
	}
}

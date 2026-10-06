package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FuzzCompiledEndpointBody checks that arbitrary decode outcomes cannot poison
// the pooled lazy body state used by a subsequently handled request.
func FuzzCompiledEndpointBody(f *testing.F) {
	for _, seed := range []string{
		`{"name":"seed"}`,
		`{`,
		`null`,
		`{"unknown":1}`,
		`{"name":"a"} trailing`,
	} {
		f.Add(seed)
	}

	s := NewServer(WithMaxBodyBytes(4))
	if err := s.Register(Post("/echo", func(ctx context.Context, req RequestOf[routePoolBody]) (routePoolBody, error) {
		return req.Data(ctx)
	}, WithUnlimitedBody())); err != nil {
		f.Fatalf("Register failed: %v", err)
	}

	serve := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	f.Fuzz(func(t *testing.T, input string) {
		first := serve(input)
		if first.Code != http.StatusOK && first.Code != http.StatusBadRequest {
			t.Fatalf("arbitrary input status = %d, want 200 or 400; body=%q", first.Code, first.Body.String())
		}

		stable := serve(`{"name":"stable"}`)
		if stable.Code != http.StatusOK || stable.Body.String() != `{"name":"stable"}` {
			t.Fatalf("pooled state after arbitrary input = %d %q", stable.Code, stable.Body.String())
		}

		repeat := serve(`{"name":"repeat"}`)
		if repeat.Code != http.StatusOK || repeat.Body.String() != `{"name":"repeat"}` {
			t.Fatalf("pooled state on repeated valid input = %d %q", repeat.Code, repeat.Body.String())
		}
	})
}

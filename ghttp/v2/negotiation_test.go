package v2

import (
	"context"
	root "github.com/sofiworker/gk/ghttp"
	"net/http/httptest"
	"testing"
)

func TestNegotiation(t *testing.T) {
	for _, tc := range []struct {
		accept, ct string
		status     int
	}{{"", "application/json; charset=utf-8", 200}, {"application/xml", "application/xml; charset=utf-8", 200}, {"application/json;q=0, */*;q=1", "application/xml; charset=utf-8", 200}, {"application/json;q=0.2,application/xml;q=0.8", "application/xml; charset=utf-8", 200}, {"image/png", "", 406}, {"*/*;q=0", "", 406}, {"application/*", "application/json; charset=utf-8", 200}} {
		t.Run(tc.accept, func(t *testing.T) {
			s := root.New()
			called := false
			route := Get("/", func(context.Context, RequestInput) (string, error) { called = true; return "ok", nil }, WithNegotiation(JSONOutput[string](), XMLOutput[string]()))
			if err := route.Mount(s); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			if tc.accept != "" {
				req.Header.Set("Accept", tc.accept)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			if tc.status == 200 && rec.Header().Get("Content-Type") != tc.ct {
				t.Fatal(rec.Header())
			}
			if tc.status == 406 && called {
				t.Fatal("handler called for unacceptable representation")
			}
			if rec.Header().Get("Vary") != "Accept" {
				t.Fatal(rec.Header())
			}
		})
	}
}

package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutoHEADFallsBackToGET(t *testing.T) {
	s := New(WithAutoHEAD(true))
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, r *Response) error {
		r.Header().Set("X-Test", "yes")
		r.Write([]byte("body"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/x", nil))
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("X-Test") != "yes" {
		t.Fatalf("status=%d body=%q headers=%v", w.Code, w.Body.String(), w.Header())
	}
}

func TestAutoHEADExplicitRouteWins(t *testing.T) {
	s := New(WithAutoHEAD(true))
	get := func(_ context.Context, _ *Request, r *Response) error { r.Write([]byte("get")); return nil }
	head := func(_ context.Context, _ *Request, r *Response) error { r.Header().Set("X-Explicit", "1"); return nil }
	if err := s.RawHandle(http.MethodGet, "/x", get); err != nil {
		t.Fatal(err)
	}
	if err := s.RawHandle(http.MethodHead, "/x", head); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/x", nil))
	if w.Header().Get("X-Explicit") != "1" {
		t.Fatal("explicit HEAD route was not selected")
	}
}

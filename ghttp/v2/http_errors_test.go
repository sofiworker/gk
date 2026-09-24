package v2

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestHTTPErrorAndRecovery(t *testing.T) {
	marker := errors.New("denied")
	if !errors.Is(HTTPError{Status: 403, Cause: marker}, marker) {
		t.Fatal("lost cause")
	}
	for _, status := range []int{0, 200, 403, 700} {
		s := NewServer()
		if err := s.Register(FromFunc("GET", "/", func(context.Context) (string, error) { return "", HTTPError{Status: status, Cause: marker} })); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		want := 500
		if status == 403 {
			want = 403
		}
		if rec.Code != want {
			t.Fatalf("status %d", rec.Code)
		}
	}
	s := NewServer().Use(Recovery())
	if err := s.Register(FromFunc("GET", "/", func(context.Context) (string, error) { panic("failure") })); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 {
		t.Fatalf("status %d", rec.Code)
	}
}

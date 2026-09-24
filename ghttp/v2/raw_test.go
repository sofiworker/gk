package v2

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRawRoute(t *testing.T) {
	for _, method := range []string{"POST", "HEAD"} {
		s := NewServer()
		calls := 0
		g := s.Group("/api").With(WithBodyLimit(3), WithInput(JSONInput[int]())).Use(func(next Handler) Handler {
			return func(c context.Context, r *Request, w *Response) error { calls++; return next(c, r, w) }
		})
		route := Raw(method, "/raw", func(_ context.Context, r *Request, w *Response) error {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				return err
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(202)
			_, err = w.Write(b)
			return err
		})
		if err := g.Register(route); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(method, "/api/raw", strings.NewReader("abc")))
		want := "abc"
		if method == "HEAD" {
			want = ""
		}
		if rec.Code != 202 || rec.Body.String() != want || calls != 1 {
			t.Fatalf("%d %q %d", rec.Code, rec.Body.String(), calls)
		}
		rec = httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(method, "/api/raw", strings.NewReader("abcd")))
		if rec.Code != 413 {
			t.Fatalf("limit: %d", rec.Code)
		}
	}
}
func TestRawErrors(t *testing.T) {
	for _, committed := range []bool{false, true} {
		s := NewServer()
		marker := errors.New("failed")
		if err := s.Register(Raw("GET", "/", func(_ context.Context, r *Request, w *Response) error {
			if committed {
				_, _ = w.Write([]byte("partial"))
			}
			return HTTPError{Status: 403, Cause: marker}
		})); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		if committed {
			if rec.Code != 200 || rec.Body.String() != "partial" {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		} else if rec.Code != 403 {
			t.Fatalf("%d", rec.Code)
		}
	}
	if Raw("GET", "/", nil).Err() == nil {
		t.Fatal("nil handler accepted")
	}
	if Raw("POST", "/", func(context.Context, *Request, *Response) error { return nil }, WithOutput(JSONOutput[int]())).Err() == nil {
		t.Fatal("codec accepted")
	}
}

package ghttp

import (
	"bytes"
	"context"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestJSONTrailingContent(t *testing.T) {
	for _, body := range []string{`{"ok":true}]`, `{"ok":true}}`, `{"ok":true}]garbage`} {
		t.Run(body, func(t *testing.T) {
			r := &Request{Request: httptest.NewRequest("POST", "/", strings.NewReader(body))}
			var value map[string]any
			if err := JSONCodec().Decode(r, &value); err == nil {
				t.Fatalf("invalid JSON accepted: %q, decoded=%v", body, value)
			}
		})
	}
}

func TestJSONLimitAfterValue(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`+strings.Repeat(" ", 2048)))
	r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 1024)
	var value map[string]any
	if err := JSONCodec().Decode(&Request{Request: r}, &value); err == nil {
		t.Fatal("body exceeded limit after JSON value but decoder returned success")
	}
}

func TestInformationalResponse(t *testing.T) {
	s := New()
	if err := s.RawHandle("GET", "/", func(_ context.Context, _ *Request, w *Response) error {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
		_, err := w.Write([]byte("created"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()
	r, err := ts.Client().Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("final status=%d, want 201; body=%q", r.StatusCode, body)
	}
}

func TestJSONEncodingFailureStatus(t *testing.T) {
	s := New()
	if err := GetNone(s, "/", JSON[float64](), func(context.Context) (float64, error) {
		return math.NaN(), nil
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("encoding failed but status=%d, body=%q; want 500", w.Code, w.Body.String())
	}
}

func TestTimeoutMultipartCleanup(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		name := "without_timeout"
		if timeout {
			name = "with_timeout"
		}
		t.Run(name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			s := New()
			if timeout {
				s.Use(Timeout(time.Minute))
			}
			if err := s.RawHandle("POST", "/", func(_ context.Context, r *Request, w *Response) error {
				if err := r.ParseMultipartForm(1); err != nil {
					return err
				}
				w.WriteHeader(http.StatusNoContent)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(s)
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			part, err := mw.CreateFormFile("file", "sample.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(part, strings.Repeat("x", 1024)); err != nil {
				t.Fatal(err)
			}
			if err := mw.Close(); err != nil {
				t.Fatal(err)
			}
			r, err := ts.Client().Post(ts.URL, mw.FormDataContentType(), &body)
			if err != nil {
				ts.Close()
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			ts.Close()
			if r.StatusCode != http.StatusNoContent {
				t.Fatalf("status=%d", r.StatusCode)
			}
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("multipart files leaked after server closed: %v", entries)
			}
		})
	}
}

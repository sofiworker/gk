package v2

import (
	"bytes"
	"context"
	"errors"
	root "github.com/sofiworker/gk/ghttp"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCustomMultipartCleanup(t *testing.T) {
	for _, stage := range []string{"decoder", "handler", "panic", "raw"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "test.bin")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = part.Write([]byte("contents")); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			marker := errors.New("test failure")
			parse := func(r *Request) error {
				if err := r.ParseMultipartForm(0); err != nil {
					return err
				}
				files, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				if len(files) == 0 {
					t.Fatal("expected spilled file")
				}
				return nil
			}
			var route Route
			if stage == "raw" {
				route = Raw("POST", "/", func(_ context.Context, r *Request, _ *Response) error {
					if err := parse(r); err != nil {
						return err
					}
					return marker
				})
			} else if stage == "decoder" {
				route = Post("/", func(context.Context, struct{}) (string, error) { t.Fatal("handler called"); return "", nil }, WithInput(DecodeWith(func(_ context.Context, r *Request) (struct{}, error) {
					if err := parse(r); err != nil {
						return struct{}{}, err
					}
					return struct{}{}, marker
				})))
			} else {
				route = Post("/", func(_ context.Context, in RequestInput) (string, error) {
					if err := parse(in.Request); err != nil {
						return "", err
					}
					if stage == "panic" {
						panic(marker)
					}
					return "", marker
				})
			}
			req := httptest.NewRequest("POST", "/", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			func() {
				defer func() {
					if p := recover(); p != nil {
						if stage != "panic" || p != marker {
							t.Fatalf("unexpected panic: %v", p)
						}
					} else if stage == "panic" {
						t.Fatal("expected panic")
					}
				}()
				err := route.Serve(context.Background(), &Request{Request: req}, &Response{ResponseWriter: httptest.NewRecorder()})
				if !errors.Is(err, marker) {
					t.Fatalf("error: %v", err)
				}
			}()
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("leaked files %v: %v", files, err)
			}
		})
	}
}

func TestRequestInputBodyLimit(t *testing.T) {
	route := Post("/", func(_ context.Context, in RequestInput) (string, error) {
		data, err := io.ReadAll(in.Body)
		return string(data), err
	}, WithBodyLimit(3))
	err := route.Serve(context.Background(), &Request{Request: httptest.NewRequest("POST", "/", strings.NewReader("1234"))}, &Response{ResponseWriter: httptest.NewRecorder()})
	if !errors.Is(err, root.ErrRequestEntityTooLarge) {
		t.Fatalf("expected body limit: %v", err)
	}
}

package v2

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
)

func TestMultipartStreamInput(t *testing.T) {
	for _, limit := range []int64{32, 4096} {
		t.Run(strconv.FormatInt(limit, 10), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			var body bytes.Buffer
			w := multipart.NewWriter(&body)
			if err := w.WriteField("title", "example"); err != nil {
				t.Fatal(err)
			}
			p, err := w.CreateFormFile("file", "a.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = p.Write([]byte("payload")); err != nil {
				t.Fatal(err)
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			route := Post("/", func(_ context.Context, r *multipart.Reader) (string, error) {
				var result string
				for {
					part, err := r.NextPart()
					if err == io.EOF {
						return result, nil
					}
					if err != nil {
						return "", err
					}
					data, err := io.ReadAll(part)
					closeErr := part.Close()
					if err != nil {
						return "", err
					}
					if closeErr != nil {
						return "", closeErr
					}
					result += string(data)
				}
			}, WithInput(MultipartStreamInput()), WithBodyLimit(limit))
			s := NewServer()
			if err := s.Register(route); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/", &body)
			req.Header.Set("Content-Type", w.FormDataContentType())
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if limit == 32 && rec.Code != 413 {
				t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
			}
			if limit > 32 && (rec.Code != 200 || rec.Body.String() != "\"examplepayload\"\n") {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("unexpected temporary files: %v %v", entries, err)
			}
		})
	}
}

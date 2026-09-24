package v2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMultipartLifecycle(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "handler_error"}[fail], func(t *testing.T) {
			var body bytes.Buffer
			w := multipart.NewWriter(&body)
			_ = w.WriteField("title", "example")
			for _, name := range []string{"file", "files", "files"} {
				p, err := w.CreateFormFile(name, "test.txt")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = p.Write([]byte("contents"))
			}
			_ = w.Close()
			type upload struct {
				Title string                  `form:"title"`
				File  *multipart.FileHeader   `form:"file"`
				Files []*multipart.FileHeader `form:"files"`
			}
			var filePath string
			marker := errors.New("handler failure")
			r := Post("/", func(_ context.Context, in *upload) (string, error) {
				if in.Title != "example" || in.File == nil || len(in.Files) != 2 {
					t.Fatalf("input: %+v", in)
				}
				f, err := in.File.Open()
				if err != nil {
					t.Fatal(err)
				}
				if disk, ok := f.(*os.File); ok {
					filePath = disk.Name()
				}
				data, err := io.ReadAll(f)
				_ = f.Close()
				if err != nil || string(data) != "contents" {
					t.Fatalf("read %q: %v", data, err)
				}
				if fail {
					return "", marker
				}
				return "ok", nil
			}, WithInput(MultipartInput[*upload](MultipartLimits{MaxBytes: 1 << 20, MemoryBytes: 0})))
			req := httptest.NewRequest("POST", "/", &body)
			req.Header.Set("Content-Type", w.FormDataContentType())
			wrapped := &Request{Request: req}
			err := r.Serve(context.Background(), wrapped, &Response{ResponseWriter: httptest.NewRecorder()})
			if fail && !errors.Is(err, marker) || !fail && err != nil {
				t.Fatal(err)
			}
			if filePath != "" {
				if _, err := os.Stat(filePath); !os.IsNotExist(err) {
					t.Fatalf("temporary file remains: %v", err)
				}
			}
			if _, err := wrapped.MultipartForm.File["file"][0].Open(); err == nil {
				t.Fatal("temporary upload still accessible")
			}
		})
	}
}

func TestMultipartPlanNestedTextAndFiles(t *testing.T) {
	type nested struct {
		Count  *int                  `form:"count"`
		Codes  []bindingTextValue    `form:"code"`
		Upload *multipart.FileHeader `form:"upload"`
	}
	type input struct{ Nested *nested }
	var empty bytes.Buffer
	w := multipart.NewWriter(&empty)
	_ = w.Close()
	in := MultipartInput[input]()
	var absent input
	r := httptest.NewRequest("POST", "/", &empty)
	r.Header.Set("Content-Type", w.FormDataContentType())
	if err := in.Decode(&Request{Request: r}, &absent); err != nil || absent.Nested != nil {
		t.Fatalf("missing fields: value=%+v err=%v", absent, err)
	}
	var body bytes.Buffer
	w = multipart.NewWriter(&body)
	_ = w.WriteField("count", "7")
	_ = w.WriteField("code", "2")
	_ = w.WriteField("code", "3")
	part, err := w.CreateFormFile("upload", "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("data"))
	_ = w.Close()
	r = httptest.NewRequest("POST", "/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	var got input
	if err := in.Decode(&Request{Request: r}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Nested == nil || got.Nested.Count == nil || *got.Nested.Count != 7 ||
		!reflect.DeepEqual(got.Nested.Codes, []bindingTextValue{3, 4}) ||
		got.Nested.Upload == nil || got.Nested.Upload.Filename != "file.txt" {
		t.Fatalf("value=%+v", got)
	}
}

func TestMultipartPlanRejectsRecursiveType(t *testing.T) {
	type recursive struct{ Next *recursive }
	decoder := multipartDecoder[recursive]{}
	in := MultipartInput[recursive]()
	decoder = in.decoder.(multipartDecoder[recursive])
	if err := decoder.registrationError(); err == nil || !strings.Contains(err.Error(), "recursive") {
		t.Fatalf("expected recursive type error, got %v", err)
	}
}

func TestMultipartBodyLimit(t *testing.T) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	p, _ := w.CreateFormFile("file", "test")
	_, _ = p.Write(bytes.Repeat([]byte("x"), 1024))
	_ = w.Close()
	type upload struct {
		File *multipart.FileHeader `form:"file"`
	}
	r := Post("/", func(context.Context, upload) (string, error) { t.Fatal("handler called"); return "", nil }, WithInput(MultipartInput[upload](MultipartLimits{MaxBytes: 100, MemoryBytes: 0})))
	req := httptest.NewRequest("POST", "/", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	err := r.Serve(context.Background(), &Request{Request: req}, &Response{ResponseWriter: httptest.NewRecorder()})
	var limit *http.MaxBytesError
	if !errors.As(err, &limit) {
		t.Fatalf("expected size limit: %v", err)
	}
}

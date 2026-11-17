package client

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestMultipartFormDataAndFile 验证文本字段与文件路径部件都能正确送达。
func TestMultipartFormDataAndFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "upload.txt")
	if err := os.WriteFile(filePath, []byte("file-body"), 0o600); err != nil {
		t.Fatal(err)
	}

	var gotField, gotFileName, gotFileBody, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("server ParseMultipartForm: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotField = r.FormValue("title")
		f, header, err := r.FormFile("doc")
		if err == nil {
			defer func() { _ = f.Close() }()
			gotFileName = header.Filename
			gotContentType = header.Header.Get("Content-Type")
			data, _ := io.ReadAll(f)
			gotFileBody = string(data)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.R().
		SetMultipartFormData(map[string]string{"title": "hello"}).
		SetFile("doc", filePath).
		Post("/upload")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotField != "hello" {
		t.Fatalf("text field = %q", gotField)
	}
	if gotFileName != "upload.txt" || gotFileBody != "file-body" {
		t.Fatalf("file = %q / %q", gotFileName, gotFileBody)
	}
	if gotContentType == "" {
		t.Fatal("the part should carry a Content-Type")
	}
}

// TestMultipartFieldWithReaderAndContentType 验证 reader 型部件与显式 Content-Type。
func TestMultipartFieldWithReaderAndContentType(t *testing.T) {
	var gotContentType, gotFileName, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		f, header, err := r.FormFile("blob")
		if err != nil {
			t.Errorf("FormFile: %v", err)
			return
		}
		defer func() { _ = f.Close() }()
		gotFileName = header.Filename
		gotContentType = header.Header.Get("Content-Type")
		data, _ := io.ReadAll(f)
		gotBody = string(data)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.R().
		SetMultipartField(MultipartField{
			Field:       "blob",
			FileName:    "note.txt",
			ContentType: "text/plain",
			Reader:      strings.NewReader("reader-body"),
		}).
		Post("/upload")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotBody != "reader-body" || gotFileName != "note.txt" || gotContentType != "text/plain" {
		t.Fatalf("part = body:%q name:%q ct:%q", gotBody, gotFileName, gotContentType)
	}
}

// TestMultipartRejectsUnreplayableReaderWithRetry 验证 multipart 里不可 seek 的 reader
// 在开启重试时被发送前拒绝。
func TestMultipartRejectsUnreplayableReaderWithRetry(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 3, RetryDelay: time.Millisecond, RetryNonIdempotent: true}),
	)
	_, err := c.R().
		// bytes.Buffer 不实现 io.Seeker，这才是真正的不可重放载体。
		// A bytes.Buffer does not implement io.Seeker — a genuinely unreplayable carrier.
		SetFileReader("doc", "a.txt", bytes.NewBufferString("payload")).
		Post("/upload")
	if !errors.Is(err, ErrBodyNotReplayable) {
		t.Fatalf("err = %v, want ErrBodyNotReplayable", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 0 {
		t.Fatalf("server saw %d requests; must be refused before the first attempt", got)
	}
}

// TestMultipartReplaysFileOnRetry 验证路径型文件部件在重试时被重新打开，内容一致。
func TestMultipartReplaysFileOnRetry(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "retry.txt")
	if err := os.WriteFile(filePath, []byte("stable-body"), 0o600); err != nil {
		t.Fatal(err)
	}

	var attempts int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			if f, _, err := r.FormFile("doc"); err == nil {
				data, _ := io.ReadAll(f)
				_ = f.Close()
				bodies = append(bodies, string(data))
			}
		}
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 2, RetryDelay: time.Millisecond, RetryNonIdempotent: true}),
	)
	if _, err := c.R().SetFile("doc", filePath).Post("/upload"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("server saw %d uploads, want 2", len(bodies))
	}
	for i, b := range bodies {
		if b != "stable-body" {
			t.Fatalf("attempt %d body = %q", i+1, b)
		}
	}
}

// TestMultipartSeekableReaderRewound 验证可 seek 的 reader 部件在重试前归零。
func TestMultipartSeekableReaderRewound(t *testing.T) {
	var attempts int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			if f, _, err := r.FormFile("doc"); err == nil {
				data, _ := io.ReadAll(f)
				_ = f.Close()
				bodies = append(bodies, string(data))
			}
		}
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(
		WithBaseURL(srv.URL),
		WithRetry(RetryPolicy{MaxRetries: 2, RetryDelay: time.Millisecond, RetryNonIdempotent: true}),
	)
	if _, err := c.R().
		SetMultipartField(MultipartField{Field: "doc", FileName: "s.txt", Reader: bytes.NewReader([]byte("seek-body"))}).
		Post("/upload"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("uploads = %d, want 2", len(bodies))
	}
	for i, b := range bodies {
		if b != "seek-body" {
			t.Fatalf("attempt %d body = %q (reader was not rewound)", i+1, b)
		}
	}
}

// TestMultipartBoundaryOverride 验证自定义 boundary 被使用。
func TestMultipartBoundaryOverride(t *testing.T) {
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.R().SetMultipartBoundary("my-boundary-123").SetMultipartFormData(map[string]string{"a": "b"}).Post("/x"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if !strings.Contains(contentType, "my-boundary-123") {
		t.Fatalf("Content-Type = %q, want the custom boundary", contentType)
	}
}

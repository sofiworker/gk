package ghttp

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReply_BasicFields(t *testing.T) {
	reply := Reply[string]{
		Body:   "test body",
		Status: http.StatusCreated,
		Headers: http.Header{
			"X-Custom-Header": []string{"custom-value"},
		},
		Cookies: []*http.Cookie{
			{Name: "session", Value: "abc123", Path: "/"},
		},
	}

	if reply.Body != "test body" {
		t.Errorf("Reply.Body = %q, want %q", reply.Body, "test body")
	}
	if reply.Status != http.StatusCreated {
		t.Errorf("Reply.Status = %d, want %d", reply.Status, http.StatusCreated)
	}
	if reply.Headers.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("Reply.Headers incorrect")
	}
	if len(reply.Cookies) != 1 || reply.Cookies[0].Name != "session" {
		t.Errorf("Reply.Cookies incorrect")
	}
}

func TestFileReply_ServeFile_FromPath(t *testing.T) {
	// 创建临时文件
	// Create temporary file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test file content")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	fileReply := FileReply{
		Path:        tmpFile,
		ContentType: "text/plain",
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)

	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	if err := fileReply.ServeFile(resp, ghttpReq); err != nil {
		t.Fatalf("ServeFile() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Body.String(); got != string(content) {
		t.Errorf("Response body = %q, want %q", got, string(content))
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/plain")
	}
}

func TestFileReply_ServeFile_FromReader(t *testing.T) {
	content := []byte("reader content")
	reader := bytes.NewReader(content)

	fileReply := FileReply{
		Reader:      reader,
		Name:        "test.txt",
		ContentType: "text/plain",
		Size:        int64(len(content)),
		ModTime:     time.Now(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)

	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	if err := fileReply.ServeFile(resp, ghttpReq); err != nil {
		t.Fatalf("ServeFile() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Body.String(); got != string(content) {
		t.Errorf("Response body = %q, want %q", got, string(content))
	}
}

func TestFileReply_ServeFile_Attachment(t *testing.T) {
	content := []byte("download me")
	reader := bytes.NewReader(content)

	fileReply := FileReply{
		Reader:     reader,
		Name:       "download.txt",
		Size:       int64(len(content)),
		ModTime:    time.Now(),
		Attachment: true,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/download", nil)

	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	if err := fileReply.ServeFile(resp, ghttpReq); err != nil {
		t.Fatalf("ServeFile() error = %v", err)
	}

	disposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment") {
		t.Errorf("Content-Disposition = %q, want to contain 'attachment'", disposition)
	}
	if !strings.Contains(disposition, "download.txt") {
		t.Errorf("Content-Disposition = %q, want to contain 'download.txt'", disposition)
	}
}

func TestFileReply_ServeFile_NotFound(t *testing.T) {
	fileReply := FileReply{
		Path: "/nonexistent/file.txt",
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)

	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	err := fileReply.ServeFile(resp, ghttpReq)
	if err == nil {
		t.Fatal("ServeFile() expected error, got nil")
	}

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusNotFound {
		t.Errorf("HTTPError.Status = %d, want %d", httpErr.Status, http.StatusNotFound)
	}
}

func TestFileReply_ServeFile_NoPathNoReader(t *testing.T) {
	fileReply := FileReply{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)

	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	err := fileReply.ServeFile(resp, ghttpReq)
	if err == nil {
		t.Fatal("ServeFile() expected error, got nil")
	}

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusBadRequest {
		t.Errorf("HTTPError.Status = %d, want %d", httpErr.Status, http.StatusBadRequest)
	}
}

func TestStreamReply_Stream(t *testing.T) {
	streamReply := StreamReply{
		ContentType: "text/event-stream",
		Writer: func(w io.Writer) error {
			_, err := w.Write([]byte("data: test\n\n"))
			return err
		},
	}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := streamReply.Stream(resp); err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}

	if got := rec.Body.String(); got != "data: test\n\n" {
		t.Errorf("Response body = %q, want %q", got, "data: test\\n\\n")
	}
}

func TestStreamReply_Stream_DefaultContentType(t *testing.T) {
	streamReply := StreamReply{
		Writer: func(w io.Writer) error {
			_, err := w.Write([]byte("plain text"))
			return err
		},
	}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := streamReply.Stream(resp); err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/plain; charset=utf-8")
	}
}

func TestRedirectReply_Redirect(t *testing.T) {
	redirectReply := RedirectReply{
		URL:    "/new-location",
		Status: http.StatusMovedPermanently,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/old-location", nil)

	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	redirectReply.Redirect(resp, ghttpReq)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}

	if loc := rec.Header().Get("Location"); loc != "/new-location" {
		t.Errorf("Location header = %q, want %q", loc, "/new-location")
	}
}

func TestRedirectReply_Redirect_DefaultStatus(t *testing.T) {
	redirectReply := RedirectReply{
		URL: "/new-location",
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/old-location", nil)

	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	redirectReply.Redirect(resp, ghttpReq)

	if rec.Code != http.StatusFound {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusFound)
	}
}

func TestNoContentReply_WriteNoContent(t *testing.T) {
	noContentReply := NoContentReply{}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	noContentReply.WriteNoContent(resp)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	if rec.Body.Len() != 0 {
		t.Errorf("Response body length = %d, want 0", rec.Body.Len())
	}
}

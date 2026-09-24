package v2

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	root "github.com/sofiworker/gk/ghttp"
)

func serveHTTPReply[T any](t *testing.T, method string, request *http.Request, reply T) (*httptest.ResponseRecorder, error) {
	t.Helper()
	route := FromFunc(method, "/content", func(context.Context) (T, error) { return reply, nil })
	rec := httptest.NewRecorder()
	err := route.Serve(context.Background(), &Request{Request: request}, &Response{ResponseWriter: rec})
	return rec, err
}

func TestFileReplyHTTPConditions(t *testing.T) {
	modified := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name, method, rangeHeader, modifiedSince string
		status                                   int
		body                                     string
	}{
		{name: "whole", method: http.MethodGet, status: http.StatusOK, body: "abcdef"},
		{name: "range", method: http.MethodGet, rangeHeader: "bytes=1-3", status: http.StatusPartialContent, body: "bcd"},
		{name: "unsatisfiable", method: http.MethodGet, rangeHeader: "bytes=99-", status: http.StatusRequestedRangeNotSatisfiable},
		{name: "not modified", method: http.MethodGet, modifiedSince: modified.Format(http.TimeFormat), status: http.StatusNotModified},
		{name: "head", method: http.MethodHead, status: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/content", nil)
			req.Header.Set("Range", tc.rangeHeader)
			req.Header.Set("If-Modified-Since", tc.modifiedSince)
			rec, err := serveHTTPReply(t, tc.method, req, FileReply{
				Request: req, Name: "report.txt", Content: strings.NewReader("abcdef"), ModTime: modified,
				DownloadName: "report 2025.txt",
			})
			if err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.status || tc.status != http.StatusRequestedRangeNotSatisfiable && rec.Body.String() != tc.body {
				t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="report 2025.txt"` {
				t.Fatalf("disposition=%q", got)
			}
			if tc.name == "range" && rec.Header().Get("Content-Range") != "bytes 1-3/6" {
				t.Fatalf("range=%q", rec.Header().Get("Content-Range"))
			}
			if tc.name == "head" && rec.Header().Get("Content-Length") != "6" {
				t.Fatalf("head length=%q", rec.Header().Get("Content-Length"))
			}
		})
	}
}

func TestHTTPReplyValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/content", nil)
	tests := []struct {
		name  string
		value interface{ encodeReply(*Response) error }
	}{
		{name: "file", value: FileReply{Request: req}},
		{name: "redirect location", value: RedirectReply{}},
		{name: "redirect CRLF", value: RedirectReply{Location: "/next\r\nX-Injected: 1"}},
		{name: "redirect status", value: RedirectReply{Location: "/next", Status: 200}},
		{name: "stream reader", value: StreamReply{}},
		{name: "stream status", value: StreamReply{Reader: strings.NewReader("ok"), Status: 99}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &Response{ResponseWriter: httptest.NewRecorder()}
			if err := tc.value.encodeReply(resp); err == nil || resp.Written() {
				t.Fatalf("error=%v committed=%v", err, resp.Written())
			}
		})
	}
}

func TestRedirectReply(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/content", nil)
	for _, status := range []int{0, http.StatusPermanentRedirect} {
		rec, err := serveHTTPReply(t, http.MethodGet, req, RedirectReply{Location: "/next", Status: status})
		if err != nil {
			t.Fatal(err)
		}
		want := status
		if want == 0 {
			want = http.StatusFound
		}
		if rec.Code != want || rec.Header().Get("Location") != "/next" || rec.Body.Len() != 0 {
			t.Fatalf("status=%d header=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
		}
	}
}

type failReader struct{ err error }

func (r failReader) Read([]byte) (int, error) { return 0, r.err }

func TestStreamReply(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/content", nil)
	rec, err := serveHTTPReply(t, http.MethodGet, req, StreamReply{Request: req, Reader: strings.NewReader("stream"), ContentType: "text/plain", Status: http.StatusAccepted})
	if err != nil || rec.Code != http.StatusAccepted || rec.Body.String() != "stream" || rec.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("status=%d body=%q error=%v", rec.Code, rec.Body.String(), err)
	}
	readErr := errors.New("read failed")
	rec, err = serveHTTPReply(t, http.MethodGet, req, StreamReply{Reader: failReader{readErr}})
	if !errors.Is(err, readErr) || rec.Body.Len() != 0 {
		t.Fatalf("error=%v body=%q", err, rec.Body.String())
	}
	head := httptest.NewRequest(http.MethodHead, "/content", nil)
	rec, err = serveHTTPReply(t, http.MethodHead, head, StreamReply{Request: head, Reader: failReader{readErr}, Status: http.StatusOK})
	if err != nil || rec.Body.Len() != 0 {
		t.Fatalf("head error=%v body=%q", err, rec.Body.String())
	}
	rec, err = serveHTTPReply(t, http.MethodGet, req, StreamReply{Reader: failReader{readErr}, Status: http.StatusNoContent})
	if err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("no content status=%d error=%v", rec.Code, err)
	}
}

type trackedContent struct {
	*strings.Reader
	closed   bool
	closeErr error
}

func (c *trackedContent) Close() error {
	c.closed = true
	return c.closeErr
}

func TestHTTPReplyClosesContent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/content", nil)
	file := &trackedContent{Reader: strings.NewReader("file")}
	_, err := serveHTTPReply(t, http.MethodGet, req, FileReply{Request: req, Name: "file.txt", Content: file, Closer: file})
	if err != nil || !file.closed {
		t.Fatalf("file error=%v closed=%v", err, file.closed)
	}

	invalid := &trackedContent{Reader: strings.NewReader("invalid")}
	_, err = serveHTTPReply(t, http.MethodGet, req, FileReply{Content: invalid, Closer: invalid})
	if err == nil || !invalid.closed {
		t.Fatalf("invalid file error=%v closed=%v", err, invalid.closed)
	}

	head := httptest.NewRequest(http.MethodHead, "/content", nil)
	headFile := &trackedContent{Reader: strings.NewReader("head file")}
	_, err = serveHTTPReply(t, http.MethodHead, head, FileReply{Request: head, Name: "head.txt", Content: headFile, Closer: headFile})
	if err != nil || !headFile.closed {
		t.Fatalf("head file error=%v closed=%v", err, headFile.closed)
	}

	headStream := &trackedContent{Reader: strings.NewReader("not read")}
	_, err = serveHTTPReply(t, http.MethodHead, head, StreamReply{Request: head, Reader: headStream, Closer: headStream})
	if err != nil || !headStream.closed || headStream.Len() != len("not read") {
		t.Fatalf("head error=%v closed=%v remaining=%d", err, headStream.closed, headStream.Len())
	}

	readErr := errors.New("read failed")
	stream := &trackedContent{Reader: strings.NewReader(""), closeErr: io.ErrClosedPipe}
	_, err = serveHTTPReply(t, http.MethodGet, req, StreamReply{Reader: failReader{readErr}, Closer: stream})
	if !stream.closed || !errors.Is(err, readErr) || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("stream error=%v closed=%v", err, stream.closed)
	}
}

func TestHTTPReplyMountedContentTypes(t *testing.T) {
	server := root.New()
	request := httptest.NewRequest(http.MethodGet, "/file", nil)
	file := FromFunc(http.MethodGet, "/file", func(context.Context) (FileReply, error) {
		return FileReply{Request: request, Name: "asset.unknown-v2-extension", Content: strings.NewReader("plain file")}, nil
	})
	redirect := FromFunc(http.MethodGet, "/redirect", func(context.Context) (RedirectReply, error) {
		return RedirectReply{Location: "/file"}, nil
	})
	stream := FromFunc(http.MethodGet, "/stream", func(context.Context) (StreamReply, error) {
		return StreamReply{Reader: strings.NewReader("data"), ContentType: "application/octet-stream"}, nil
	})
	for _, route := range []Route{file, redirect, stream} {
		if err := route.Mount(server); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path, contentType string
	}{
		{path: "/file", contentType: "text/plain; charset=utf-8"},
		{path: "/redirect", contentType: ""},
		{path: "/stream", contentType: "application/octet-stream"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.path == "/file" {
				req = request
			}
			server.ServeHTTP(rec, req)
			if got := rec.Result().Header.Get("Content-Type"); got != tc.contentType {
				t.Fatalf("Content-Type=%q, want %q", got, tc.contentType)
			}
		})
	}
}

package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testOutputETag = `"ghttp-k6-sample-v1"`

var testOutputModifiedTime = time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

const testOutputFixture = "ghttp k6 fixture: ASCII and Unicode 中文 🐱\n0123456789abcdefghijklmnopqrstuvwxyz\n"

func TestOutputFormats(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	tests := []struct {
		path        string
		contentType string
		body        string
	}{
		{path: "/output/json", contentType: "application/json", body: `{"message":"hello","tags":["ghttp","k6"],"meta":{"source":"typed"}}`},
		{path: "/output/xml", contentType: "application/xml", body: "<output><message>hello</message></output>"},
		{path: "/output/text", contentType: "text/plain", body: "hello text"},
		{path: "/output/binary", contentType: "application/octet-stream", body: "\x00\x01ghttp\xff"},
	}
	for _, tc := range tests {
		recorder := request(t, handler, http.MethodGet, tc.path)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", tc.path, recorder.Code)
		}
		if got := recorder.Header().Get("Content-Type"); got != tc.contentType {
			t.Fatalf("GET %s Content-Type = %q, want %q", tc.path, got, tc.contentType)
		}
		if got := strings.TrimSpace(recorder.Body.String()); got != tc.body {
			t.Fatalf("GET %s body = %q, want %q", tc.path, got, tc.body)
		}
	}
}

func TestOutputStatusesAndHeaders(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	tests := []struct {
		method   string
		path     string
		status   int
		location string
	}{
		{method: http.MethodPost, path: "/output/created", status: http.StatusCreated, location: "/output/json"},
		{method: http.MethodPost, path: "/output/accepted", status: http.StatusAccepted},
		{method: http.MethodDelete, path: "/output/empty", status: http.StatusNoContent},
		{method: http.MethodGet, path: "/output/redirect", status: http.StatusTemporaryRedirect, location: "/output/json"},
	}
	for _, tc := range tests {
		recorder := request(t, handler, tc.method, tc.path)
		if recorder.Code != tc.status || recorder.Header().Get("Location") != tc.location {
			t.Fatalf("%s %s = %d Location %q, want %d %q", tc.method, tc.path, recorder.Code, recorder.Header().Get("Location"), tc.status, tc.location)
		}
		if tc.status == http.StatusNoContent && recorder.Body.Len() != 0 {
			t.Fatalf("%s %s body = %q, want empty", tc.method, tc.path, recorder.Body.String())
		}
	}

	head := request(t, handler, http.MethodHead, "/output/json")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD /output/json = %d %q, want 200 empty body", head.Code, head.Body.String())
	}

	head = request(t, handler, http.MethodHead, "/output/empty")
	if head.Code != http.StatusMethodNotAllowed || head.Body.Len() != 0 {
		t.Fatalf("HEAD /output/empty = %d %q, want 405 empty body", head.Code, head.Body.String())
	}
}

func TestOutputHeaders(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	recorder := request(t, handler, http.MethodGet, "/output/headers")
	if recorder.Code != http.StatusOK || recorder.Body.String() != "hello" {
		t.Fatalf("GET /output/headers = %d %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Values("Vary"); len(got) != 2 || got[0] != "Accept" || got[1] != "Accept-Encoding" {
		t.Fatalf("Vary = %#v", got)
	}
	if got := recorder.Header().Values("X-Multi"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("X-Multi = %#v", got)
	}
	if got := recorder.Header().Get("Content-Length"); got != "5" {
		t.Fatalf("Content-Length = %q, want 5", got)
	}
	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want no unsupported compression claim", got)
	}
}

func TestOutputFile(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	recorder := request(t, handler, http.MethodGet, "/files/sample")
	if recorder.Code != http.StatusOK || recorder.Body.String() != testOutputFixture {
		t.Fatalf("GET /files/sample = %d %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="sample.txt"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestRange(t *testing.T) {
	data := []byte(testOutputFixture)
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	modified := testOutputModifiedTime.Format(http.TimeFormat)
	tests := []struct {
		name         string
		rangeHeader  string
		ifRange      string
		status       int
		body         string
		contentRange string
		contentLen   string
		acceptRanges string
	}{
		{name: "prefix", rangeHeader: "bytes=0-3", status: http.StatusPartialContent, body: string(data[:4]), contentRange: "bytes 0-3/" + stringInt(len(data)), contentLen: "4", acceptRanges: "bytes"},
		{name: "suffix", rangeHeader: "bytes=-5", status: http.StatusPartialContent, body: string(data[len(data)-5:]), contentRange: "bytes " + stringInt(len(data)-5) + "-" + stringInt(len(data)-1) + "/" + stringInt(len(data)), contentLen: "5", acceptRanges: "bytes"},
		{name: "open ended", rangeHeader: "bytes=5-", status: http.StatusPartialContent, body: string(data[5:]), contentRange: "bytes 5-" + stringInt(len(data)-1) + "/" + stringInt(len(data)), contentLen: stringInt(len(data) - 5), acceptRanges: "bytes"},
		{name: "invalid", rangeHeader: "bytes=invalid", status: http.StatusRequestedRangeNotSatisfiable},
		{name: "out of bounds", rangeHeader: "bytes=9999-", status: http.StatusRequestedRangeNotSatisfiable, contentRange: "bytes */" + stringInt(len(data))},
		{name: "if range date match", rangeHeader: "bytes=0-3", ifRange: modified, status: http.StatusPartialContent, body: string(data[:4]), contentRange: "bytes 0-3/" + stringInt(len(data)), contentLen: "4", acceptRanges: "bytes"},
		{name: "if range date mismatch", rangeHeader: "bytes=0-3", ifRange: "Wed, 21 Oct 2015 07:28:00 GMT", status: http.StatusOK, body: string(data), contentLen: stringInt(len(data)), acceptRanges: "bytes"},
		{name: "if range etag unsupported without response etag", rangeHeader: "bytes=0-3", ifRange: testOutputETag, status: http.StatusOK, body: string(data), contentLen: stringInt(len(data)), acceptRanges: "bytes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/files/range", nil)
			req.Header.Set("Range", tc.rangeHeader)
			if tc.ifRange != "" {
				req.Header.Set("If-Range", tc.ifRange)
			}
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.status || (tc.status != http.StatusRequestedRangeNotSatisfiable && recorder.Body.String() != tc.body) {
				t.Fatalf("status/body = %d %q, want %d %q", recorder.Code, recorder.Body.String(), tc.status, tc.body)
			}
			if got := recorder.Header().Get("Content-Range"); got != tc.contentRange {
				t.Fatalf("Content-Range = %q, want %q", got, tc.contentRange)
			}
			if tc.contentLen != "" && recorder.Header().Get("Content-Length") != tc.contentLen {
				t.Fatalf("Content-Length = %q, want %q", recorder.Header().Get("Content-Length"), tc.contentLen)
			}
			if tc.acceptRanges != "" && recorder.Header().Get("Accept-Ranges") != tc.acceptRanges {
				t.Fatalf("Accept-Ranges = %q, want %q", recorder.Header().Get("Accept-Ranges"), tc.acceptRanges)
			}
		})
	}
}

func TestETag(t *testing.T) {
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	initial := request(t, handler, http.MethodGet, "/files/etag")
	if initial.Code != http.StatusOK || initial.Header().Get("ETag") != testOutputETag || initial.Header().Get("Last-Modified") == "" {
		t.Fatalf("initial = %d ETag %q Last-Modified %q", initial.Code, initial.Header().Get("ETag"), initial.Header().Get("Last-Modified"))
	}

	check := func(name, header, value string, want int, wantETag, wantModified bool) {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/files/etag", nil)
		req.Header.Set(header, value)
		handler.ServeHTTP(recorder, req)
		if recorder.Code != want {
			t.Fatalf("%s status = %d, want %d", name, recorder.Code, want)
		}
		if (want == http.StatusNotModified || want == http.StatusPreconditionFailed) && recorder.Body.Len() != 0 {
			t.Fatalf("%s body = %q, want empty", name, recorder.Body.String())
		}
		if (recorder.Header().Get("ETag") == testOutputETag) != wantETag {
			t.Fatalf("%s ETag = %q", name, recorder.Header().Get("ETag"))
		}
		if (recorder.Header().Get("Last-Modified") != "") != wantModified {
			t.Fatalf("%s Last-Modified = %q", name, recorder.Header().Get("Last-Modified"))
		}
	}
	check("if-none-match", "If-None-Match", testOutputETag, http.StatusNotModified, true, false)
	check("if-match", "If-Match", `"different"`, http.StatusPreconditionFailed, true, true)
	check("if-modified-since", "If-Modified-Since", initial.Header().Get("Last-Modified"), http.StatusNotModified, true, false)
}

func TestOutputFixtureIndependentOfWorkingDirectory(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	directories := []string{outputTestGHTTPRoot(t), t.TempDir()}
	t.Cleanup(func() { _ = os.Chdir(original) })
	for _, directory := range directories {
		if err := os.Chdir(directory); err != nil {
			t.Fatal(err)
		}
		handler, cleanup := New(Config{})
		recorder := request(t, handler, http.MethodGet, "/files/range")
		cleanup()
		if recorder.Code != http.StatusOK || recorder.Body.String() != testOutputFixture {
			t.Fatalf("cwd %s response = %d %q", directory, recorder.Code, recorder.Body.String())
		}
	}
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}
}

func outputTestGHTTPRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve output test source")
	}
	root := filename
	for range 5 {
		root = filepath.Dir(root)
	}
	return root
}

func stringInt(value int) string { return strconv.Itoa(value) }

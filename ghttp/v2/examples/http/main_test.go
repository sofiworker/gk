package main

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExampleRoutes(t *testing.T) {
	s, err := newServer()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body, ct string
		status                 int
		contains               string
	}{
		{"GET", "/health", "", "", 200, "ok"},
		{"GET", "/api/users/7", "", "", 200, `"id":7`},
		{"PATCH", "/api/users/7", `{"name":"alice"}`, "application/json", 200, "alice"},
		{"PATCH", "/api/users/7", `{"name":""}`, "application/json", 400, ""},
		{"POST", "/api/users", `{"id":8,"name":"bob"}`, "application/json", 201, "bob"},
		{"POST", "/api/users", `{"extra":1}`, "application/json", 400, ""},
		{"DELETE", "/api/users/7", "", "", 204, ""},
		{"GET", "/api/inspect?q=test", "", "", 200, "test"},
		{"GET", "/api/decoded/9", "", "", 200, `"id":9`},
		{"POST", "/api/form", "name=alice&tag=a&tag=b", "application/x-www-form-urlencoded", 200, "alice"},
		{"GET", "/api/download", "", "", 200, "hello from v2"},
		{"GET", "/api/stream", "", "", 200, "second"},
		{"GET", "/openapi.json", "", "", 200, `"openapi": "3.1.0"`},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer demo")
			req.Header.Set("Content-Type", tc.ct)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, path := range []string{"/api/upload", "/api/upload-stream"} {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, err := mw.CreateFormFile("file", "a.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write([]byte("abc")); err != nil {
			t.Fatal(err)
		}
		if err = mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", path, &body)
		req.Header.Set("Authorization", "Bearer demo")
		req.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"bytes":3`) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("GET", "/api/users/1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("auth: %d", w.Code)
	}
	req = httptest.NewRequest("GET", "/api/download", nil)
	req.Header.Set("Authorization", "Bearer demo")
	req.Header.Set("Range", "bytes=0-4")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 206 || w.Body.String() != "hello" {
		t.Fatalf("range: %d %s", w.Code, w.Body.String())
	}
}

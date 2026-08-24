package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSecret = "never-leak-this"

func TestBindingSources(t *testing.T) {
	handler, cleanup := New(Config{MaxBodyBytes: 4096, Secret: testSecret})
	t.Cleanup(cleanup)

	tests := []struct {
		name    string
		method  string
		target  string
		body    string
		headers map[string][]string
		cookie  *http.Cookie
		want    map[string]any
	}{
		{name: "path", method: http.MethodGet, target: "/binding/path/42", want: map[string]any{"id": float64(42)}},
		{name: "query", method: http.MethodGet, target: "/binding/query?name=%E7%8C%AB%F0%9F%90%B1&count=7&enabled=1&tag=go&tag=k6", want: map[string]any{"name": "猫🐱", "count": float64(7), "enabled": true, "tags": []any{"go", "k6"}}},
		{name: "query optional int missing", method: http.MethodGet, target: "/binding/query?name=optional&enabled=false", want: map[string]any{"name": "optional", "count": float64(0), "enabled": false, "tags": []any(nil)}},
		{name: "query repeated single uses first", method: http.MethodGet, target: "/binding/query?name=first&name=second&enabled=true", want: map[string]any{"name": "first", "count": float64(0), "enabled": true, "tags": []any(nil)}},
		{name: "query int64 max and bool true", method: http.MethodGet, target: "/binding/query?name=max&count=9223372036854775807&enabled=true", want: map[string]any{"name": "max", "count": float64(9223372036854775807), "enabled": true, "tags": []any(nil)}},
		{name: "query int64 min and bool false", method: http.MethodGet, target: "/binding/query?name=min&count=-9223372036854775808&enabled=0", want: map[string]any{"name": "min", "count": float64(-9223372036854775808), "enabled": false, "tags": []any(nil)}},
		{name: "header", method: http.MethodGet, target: "/binding/header", headers: map[string][]string{"X-Trace-ID": {"trace-a", "trace-b"}, "X-Count": {"9"}}, want: map[string]any{"trace_id": "trace-a", "count": float64(9)}},
		{name: "cookie", method: http.MethodGet, target: "/binding/cookie", cookie: &http.Cookie{Name: "session", Value: "s-123"}, want: map[string]any{"session": "s-123"}},
		{name: "mixed", method: http.MethodPost, target: "/binding/mixed/8?q=hello", body: `{"name":"世界"}`, headers: map[string][]string{"Content-Type": {"application/json"}, "X-Trace-ID": {"trace-8"}}, cookie: &http.Cookie{Name: "session", Value: "s-8"}, want: map[string]any{"id": float64(8), "q": "hello", "trace_id": "trace-8", "session": "s-8", "name": "世界"}},
		{name: "time", method: http.MethodGet, target: "/binding/time?at=2026-08-09T12%3A34%3A56Z", want: map[string]any{"at": "2026-08-09T12:34:56Z"}},
		{name: "enum", method: http.MethodGet, target: "/binding/enum?status=active", want: map[string]any{"status": "active"}},
		{name: "nested equivalent", method: http.MethodGet, target: "/binding/nested?profile_name=e%CC%81&profile_city=%E6%9D%AD%E5%B7%9E", want: map[string]any{"profile": map[string]any{"name": "é", "city": "杭州"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			for name, values := range tc.headers {
				for _, value := range values {
					req.Header.Add(name, value)
				}
			}
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
			}
			assertContentType(t, recorder, "application/json")
			var got map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !equalJSON(got, tc.want) {
				t.Fatalf("response = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestBindingFailures(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	tests := []struct {
		name   string
		target string
		status int
	}{
		{name: "path syntax", target: "/binding/path/not-an-int", status: http.StatusBadRequest},
		{name: "path positive validation", target: "/binding/path/-1", status: http.StatusUnprocessableEntity},
		{name: "query required missing", target: "/binding/query?enabled=true", status: http.StatusUnprocessableEntity},
		{name: "query empty required", target: "/binding/query?name=&enabled=true", status: http.StatusUnprocessableEntity},
		{name: "query int overflow", target: "/binding/query?name=x&count=9223372036854775808&enabled=true", status: http.StatusBadRequest},
		{name: "query invalid bool", target: "/binding/query?name=x&enabled=yes", status: http.StatusBadRequest},
		{name: "enum validation", target: "/binding/enum?status=deleted", status: http.StatusUnprocessableEntity},
		{name: "time syntax", target: "/binding/time?at=yesterday", status: http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.target, nil))
			assertProblem(t, recorder, tc.status)
		})
	}
}

func TestCodecJSON(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	for _, tc := range []struct {
		name, body string
		status     int
		wantName   string
	}{
		{name: "object", body: `{"name":"alice"}`, status: 200, wantName: "alice"},
		{name: "unknown field accepted", body: `{"name":"alice","extra":true}`, status: 200, wantName: "alice"},
		{name: "truncated", body: `{"name":`, status: 400},
		{name: "empty", body: ``, status: 400},
		{name: "wrong scalar", body: `{"name":42}`, status: 400},
		{name: "one byte malformed", body: `{`, status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := serve(handler, http.MethodPost, "/codec/json", tc.body, "application/json")
			if tc.status != 200 {
				assertProblem(t, recorder, tc.status)
				return
			}
			assertJSONField(t, recorder, "name", tc.wantName)
		})
	}
}

func TestCodecXMLAndForm(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	for _, tc := range []struct {
		name, path, body, contentType string
		status                        int
		field, want                   string
	}{
		{name: "xml object", path: "/codec/xml", body: `<payload><name>alice</name></payload>`, contentType: "application/xml", status: 200, field: "name", want: "alice"},
		{name: "xml truncated", path: "/codec/xml", body: `<payload><name>alice`, contentType: "application/xml", status: 400},
		{name: "xml empty", path: "/codec/xml", body: ``, contentType: "application/xml", status: 400},
		{name: "form", path: "/codec/form", body: `name=alice&note=%E7%8C%AB`, contentType: "application/x-www-form-urlencoded", status: 200, field: "note", want: "猫"},
		{name: "form repeated uses first", path: "/codec/form", body: `name=first&name=second`, contentType: "application/x-www-form-urlencoded", status: 200, field: "name", want: "first"},
		{name: "form bad escape", path: "/codec/form", body: `name=%zz`, contentType: "application/x-www-form-urlencoded", status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := serve(handler, http.MethodPost, tc.path, tc.body, tc.contentType)
			if tc.status != 200 {
				assertProblem(t, recorder, tc.status)
				return
			}
			assertJSONField(t, recorder, tc.field, tc.want)
		})
	}
}

func TestCodecMultipart(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	checkMultipartStep(t, writer.WriteField("note", "猫"))
	part, err := writer.CreateFormFile("file", "hello.txt")
	checkMultipartStep(t, err)
	_, err = io.WriteString(part, "hello")
	checkMultipartStep(t, err)
	checkMultipartStep(t, writer.Close())
	recorder := serveBytes(handler, http.MethodPost, "/codec/multipart", body.Bytes(), writer.FormDataContentType())
	if recorder.Code != 200 {
		t.Fatalf("status = %d; body = %s", recorder.Code, recorder.Body.String())
	}
	wantHash := sha256.Sum256([]byte("hello"))
	assertJSONField(t, recorder, "filename", "hello.txt")
	assertJSONField(t, recorder, "note", "猫")
	assertJSONField(t, recorder, "hash", hex.EncodeToString(wantHash[:]))
	assertJSONNumber(t, recorder, "size", 5)

	assertProblem(t, serve(handler, http.MethodPost, "/codec/multipart", "x", "multipart/form-data"), 400)
	empty := &bytes.Buffer{}
	emptyWriter := multipart.NewWriter(empty)
	checkMultipartStep(t, emptyWriter.Close())
	assertProblem(t, serveBytes(handler, http.MethodPost, "/codec/multipart", empty.Bytes(), emptyWriter.FormDataContentType()), http.StatusBadRequest)

	many := &bytes.Buffer{}
	manyWriter := multipart.NewWriter(many)
	for _, name := range []string{"one.txt", "two.txt"} {
		part, err := manyWriter.CreateFormFile("file", name)
		checkMultipartStep(t, err)
		_, err = io.WriteString(part, "x")
		checkMultipartStep(t, err)
	}
	checkMultipartStep(t, manyWriter.Close())
	assertProblem(t, serveBytes(handler, http.MethodPost, "/codec/multipart", many.Bytes(), manyWriter.FormDataContentType()), http.StatusBadRequest)

	large := &bytes.Buffer{}
	largeWriter := multipart.NewWriter(large)
	largePart, err := largeWriter.CreateFormFile("file", "large.bin")
	checkMultipartStep(t, err)
	_, err = io.WriteString(largePart, strings.Repeat("x", 64*1024+1))
	checkMultipartStep(t, err)
	checkMultipartStep(t, largeWriter.Close())
	assertProblem(t, serveBytes(handler, http.MethodPost, "/codec/multipart", large.Bytes(), largeWriter.FormDataContentType()), http.StatusRequestEntityTooLarge)

	exact := &bytes.Buffer{}
	exactWriter := multipart.NewWriter(exact)
	exactPart, err := exactWriter.CreateFormFile("file", "exact.bin")
	checkMultipartStep(t, err)
	_, err = io.WriteString(exactPart, strings.Repeat("x", 64*1024))
	checkMultipartStep(t, err)
	checkMultipartStep(t, exactWriter.Close())
	exactRecorder := serveBytes(handler, http.MethodPost, "/codec/multipart", exact.Bytes(), exactWriter.FormDataContentType())
	assertJSONNumber(t, exactRecorder, "size", 64*1024)
}

func TestNegotiation(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	for _, tc := range []struct {
		accept, contentType string
		status              int
	}{
		{accept: "", contentType: "application/json", status: 200},
		{accept: "*/*", contentType: "application/json", status: 200},
		{accept: "application/json", contentType: "application/json", status: 200},
		{accept: "application/xml", contentType: "application/xml", status: 200},
		{accept: "application/json;q=0.1, application/xml;q=0.9", contentType: "application/xml", status: 200},
		{accept: "text/plain", contentType: "application/problem+json", status: 406},
	} {
		req := httptest.NewRequest(http.MethodPost, "/codec/negotiate", strings.NewReader(`{"name":"alice"}`))
		req.Header.Set("Content-Type", "application/json")
		if tc.accept != "" {
			req.Header.Set("Accept", tc.accept)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != tc.status {
			t.Fatalf("Accept %q status = %d, want %d", tc.accept, recorder.Code, tc.status)
		}
		if got := recorder.Header().Get("Content-Type"); got != tc.contentType {
			t.Fatalf("Accept %q Content-Type = %q, want %q", tc.accept, got, tc.contentType)
		}
		if tc.status == http.StatusNotAcceptable {
			assertProblem(t, recorder, tc.status)
			continue
		}
		if tc.contentType == "application/xml" {
			var body struct {
				Name string `xml:"name"`
			}
			if err := xml.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Name != "alice" {
				t.Fatalf("XML name = %q, want alice", body.Name)
			}
		} else {
			assertJSONField(t, recorder, "name", "alice")
		}
	}
}

func TestBodyLimit(t *testing.T) {
	handler, cleanup := New(Config{MaxBodyBytes: 4096, Secret: testSecret})
	t.Cleanup(cleanup)
	for _, size := range []int{12, 4096} {
		body := `{"data":"` + strings.Repeat("x", size-11) + `"}`
		recorder := serve(handler, http.MethodPost, "/codec/body/limited", body, "application/json")
		if recorder.Code != 200 {
			t.Fatalf("payload %d status = %d; body = %s", size, recorder.Code, recorder.Body.String())
		}
	}
	tooLarge := serve(handler, http.MethodPost, "/codec/body/limited", `{"data":"`+strings.Repeat("x", 4086)+`"}`, "application/json")
	assertProblem(t, tooLarge, http.StatusRequestEntityTooLarge)
	assertProblem(t, serve(handler, http.MethodPost, "/codec/json", `{}`, "application/octet-stream"), http.StatusUnsupportedMediaType)
}

func TestBodyLimitUsesServerDefaultWhenUnset(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	body := `{"data":"` + strings.Repeat("x", 4*1024*1024) + `"}`
	assertProblem(t, serve(handler, http.MethodPost, "/codec/body/limited", body, "application/json"), http.StatusRequestEntityTooLarge)
}

func TestBindingDoesNotChangeFrameworkErrors(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	for _, tc := range []struct {
		method, target string
		status         int
	}{
		{method: http.MethodGet, target: "/missing", status: http.StatusNotFound},
		{method: http.MethodDelete, target: "/health", status: http.StatusMethodNotAllowed},
	} {
		recorder := serve(handler, tc.method, tc.target, "", "")
		if recorder.Code != tc.status {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.target, recorder.Code, tc.status)
		}
		assertContentType(t, recorder, "application/json; charset=utf-8")
	}
}

func serve(handler http.Handler, method, target, body, contentType string) *httptest.ResponseRecorder {
	return serveBytes(handler, method, target, []byte(body), contentType)
}

func serveBytes(handler http.Handler, method, target string, body []byte, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func assertProblem(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, status, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	if strings.Contains(recorder.Body.String(), testSecret) {
		t.Fatal("response leaked Config.Secret")
	}
	var problem struct {
		Status int    `json:"status"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Status != status || problem.Title == "" || problem.Detail == "" {
		t.Fatalf("problem = %#v", problem)
	}
}

func assertJSONField(t *testing.T, recorder *httptest.ResponseRecorder, field, want string) {
	t.Helper()
	if recorder.Code != 200 {
		t.Fatalf("status = %d; body = %s", recorder.Code, recorder.Body.String())
	}
	assertContentType(t, recorder, "application/json")
	var got map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got[field] != want {
		t.Fatalf("%s = %#v, want %q", field, got[field], want)
	}
}

func assertJSONNumber(t *testing.T, recorder *httptest.ResponseRecorder, field string, want float64) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", recorder.Code, recorder.Body.String())
	}
	assertContentType(t, recorder, "application/json")
	var got map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got[field] != want {
		t.Fatalf("%s = %#v, want %v", field, got[field], want)
	}
}

func assertContentType(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := recorder.Header().Get("Content-Type"); got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
}

func checkMultipartStep(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func equalJSON(got, want any) bool {
	gotBytes, _ := json.Marshal(got)
	wantBytes, _ := json.Marshal(want)
	return bytes.Equal(gotBytes, wantBytes)
}

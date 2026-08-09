package ghttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// echoServer 返回回显请求细节的测试服务。
// echoServer returns a test server echoing request details.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Method", r.Method)
		w.Header().Set("X-ContentType", r.Header.Get("Content-Type"))
		w.Header().Set("X-Auth", r.Header.Get("Authorization"))
		w.Header().Set("X-Custom", r.Header.Get("X-Custom"))
		_ = r.ParseForm()
		w.Write([]byte(r.URL.RawQuery))
	})
	mux.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path))
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "multipart error: %v", err)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "no file: %v", err)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		w.Header().Set("X-Field", r.FormValue("note"))
		w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClientRequestSetterChain(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	resp, err := c.R().
		SetQueryParam("page", "2").
		SetQueryParams(map[string]string{"sort": "asc"}).
		SetQueryString("extra=1").
		SetHeader("X-Custom", "yes").
		SetAuthToken("tok-123").
		SetContentType("application/json").
		Get("/echo")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if resp.Header.Get("X-Auth") != "Bearer tok-123" {
		t.Fatalf("auth header = %q, want Bearer tok-123", resp.Header.Get("X-Auth"))
	}
	if resp.Header.Get("X-Custom") != "yes" {
		t.Fatalf("custom header = %q", resp.Header.Get("X-Custom"))
	}
	body := resp.String()
	for _, want := range []string{"page=2", "sort=asc", "extra=1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("query missing %q, got %q", want, body)
		}
	}
}

func TestClientPathParamReplacement(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	resp, err := c.R().
		SetPathParam("id", "u/42").
		Get("/users/{id}")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	body := resp.String()
	if body != "/users/u/42" {
		t.Fatalf("path = %q, want /users/u/42 (escaped)", body)
	}
}

func TestClientVerbMethods(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	cases := []struct {
		method string
		do     func(string) (*Response, error)
	}{
		{http.MethodGet, c.R().Get},
		{http.MethodPost, c.R().Post},
		{http.MethodPut, c.R().Put},
		{http.MethodDelete, c.R().Delete},
		{http.MethodPatch, c.R().Patch},
		{http.MethodHead, c.R().Head},
	}
	for _, tc := range cases {
		resp, err := tc.do("/echo")
		if err != nil {
			t.Fatalf("%s error = %v", tc.method, err)
		}
		if got := resp.Header.Get("X-Method"); got != tc.method {
			t.Fatalf("%s: method header = %q", tc.method, got)
		}
	}
}

func TestClientFormDataAndJSONBody(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	// form 编码。
	resp, err := c.R().SetFormData(map[string]string{"a": "1", "b": "2"}).Post("/echo")
	if err != nil {
		t.Fatalf("form Post error = %v", err)
	}
	if ct := resp.Header.Get("X-ContentType"); !strings.Contains(ct, "x-www-form-urlencoded") {
		t.Fatalf("form content-type = %q", ct)
	}

	// JSON body。
	resp, err = c.R().SetJSONBody(map[string]any{"name": "alice"}).Post("/echo")
	if err != nil {
		t.Fatalf("json Post error = %v", err)
	}
	if ct := resp.Header.Get("X-ContentType"); !strings.Contains(ct, "application/json") {
		t.Fatalf("json content-type = %q", ct)
	}
}

func TestClientMultipartFileUpload(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	resp, err := c.R().
		SetFileReader("file", "hello.txt", strings.NewReader("file-content")).
		SetFormData(map[string]string{"note": "hi"}).
		Post("/upload")
	if err != nil {
		t.Fatalf("upload error = %v", err)
	}
	if resp.Header.Get("X-Field") != "hi" {
		t.Fatalf("form field = %q, want hi", resp.Header.Get("X-Field"))
	}
	body := resp.String()
	if body != "file-content" {
		t.Fatalf("uploaded body = %q, want file-content", body)
	}
}

func TestClientMultipartSetFileOpensPath(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	// 临时文件，测试自包含，不污染仓库。
	// temp file keeps the test self-contained.
	tmp := t.TempDir()
	path := tmp + "/fixture.txt"
	if err := os.WriteFile(path, []byte("fixture content"), 0o600); err != nil {
		t.Fatal(err)
	}

	resp, err := c.R().
		SetFile("file", path).
		Post("/upload")
	if err != nil {
		t.Fatalf("upload error = %v", err)
	}
	body := resp.String()
	if !strings.Contains(body, "fixture") {
		t.Fatalf("uploaded body = %q, want fixture content", body)
	}
}

func TestClientResultBinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"u1","name":"alice"}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	type user struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var u user
	resp, err := c.R().SetResult(&u).Get("/users")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if u.ID != "u1" || u.Name != "alice" {
		t.Fatalf("bound result = %+v", u)
	}
	// String/Bytes 访问器。
	if s := resp.String(); !strings.Contains(s, "alice") {
		t.Fatalf("String() = %q", s)
	}
	if b := resp.Bytes(); len(b) == 0 {
		t.Fatal("Bytes() empty")
	}
}

func TestClientResponseMetadata(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	resp, err := c.R().SetQueryParam("size", "1").Get("/echo")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Size() <= 0 {
		t.Fatal("Size() should be positive for a non-empty body")
	}
	if resp.ReceivedAt().IsZero() {
		t.Fatal("ReceivedAt() should be set")
	}
	if resp.Time() < 0 {
		t.Fatal("Time() should not be negative")
	}
	if !resp.IsSuccess() || resp.IsError() {
		t.Fatal("IsSuccess/IsError misreported for 200")
	}
	if resp.Cookies() == nil {
		t.Fatal("Cookies() should return non-nil")
	}
	if resp.Error() != nil {
		t.Fatalf("Error() = %v, want nil", resp.Error())
	}
}

func TestClientRequestTimeoutAndContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))

	// 请求级超时。
	_, err := c.R().SetTimeout(30 * time.Millisecond).Get("/slow")
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// 请求级 ctx 取消。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.R().SetContext(ctx).Get("/slow")
	if err == nil {
		t.Fatal("expected context-cancelled error")
	}
}

func TestClientClientLevelSetters(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))
	c.SetHeader("X-Custom", "client-level").
		SetTimeout(5 * time.Second)

	// R() 继承客户端默认 header。
	resp, err := c.R().Get("/echo")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("X-Custom") != "client-level" {
		t.Fatalf("client-level header = %q", resp.Header.Get("X-Custom"))
	}
}

func TestClientRequestLevelAuth(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	// 请求级 SetAuthToken + SetAuthScheme。
	resp, err := c.R().SetAuthToken("tok").SetAuthScheme("Basic").Get("/echo")
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("X-Auth"); got != "Basic tok" {
		t.Fatalf("request auth = %q, want Basic tok", got)
	}
}

func TestClientClientLevelAuthInherited(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))
	c.SetAuthToken("tok")

	// 客户端级认证被 R() 继承（B1 回归测试）。
	// client-level auth is inherited by R() (B1 regression test).
	resp, err := c.R().Get("/echo")
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("X-Auth"); got != "Bearer tok" {
		t.Fatalf("client-level auth = %q, want Bearer tok", got)
	}

	// 请求级显式设置覆盖客户端默认。
	// an explicit request-level token overrides the client default.
	resp, err = c.R().SetAuthToken("other").Get("/echo")
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("X-Auth"); got != "Bearer other" {
		t.Fatalf("overridden auth = %q, want Bearer other", got)
	}
}

func TestClientErrorResponseBinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		json.NewEncoder(w).Encode(map[string]string{"err": "teapot"})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	var apiErr struct {
		Err string `json:"err"`
	}
	resp, err := c.R().SetError(&apiErr).Get("/fail")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if apiErr.Err != "teapot" {
		t.Fatalf("bound error = %+v", apiErr)
	}
	if !resp.IsError() {
		t.Fatal("418 should be IsError")
	}
}

func TestClientSetQueryParamsFromValues(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	values := make(map[string][]string, 1)
	values["tag"] = []string{"a", "b"}
	resp, err := c.R().
		SetQueryParamsFromValues(values).
		Get("/echo")
	if err != nil {
		t.Fatal(err)
	}
	body := resp.String()
	if !strings.Contains(body, "tag=a&tag=b") {
		t.Fatalf("multi-value query = %q", body)
	}
}

func TestClientMultipartContentTypeBoundary(t *testing.T) {
	srv := echoServer(t)
	c := NewClient(WithBaseURL(srv.URL))

	resp, err := c.R().
		SetFileReader("file", "a.txt", strings.NewReader("x")).
		Post("/echo")
	if err != nil {
		t.Fatal(err)
	}
	ct := resp.Header.Get("X-ContentType")
	if !strings.Contains(ct, "multipart/form-data") || !strings.Contains(ct, "boundary=") {
		t.Fatalf("multipart content-type = %q", ct)
	}
	var _ multipart.Form // 保证 mime/multipart 导入被使用。
}

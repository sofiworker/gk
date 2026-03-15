package gclient

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type xmlPayload struct {
	Name string `xml:"name"`
}

func TestReplacePathParams(t *testing.T) {
	path := replacePathParams("/api/:id/{name}", map[string]string{
		"id":   "42",
		"name": "foo bar",
	})
	if path != "/api/42/foo%20bar" {
		t.Fatalf("unexpected replaced path %s", path)
	}
}

func TestBuildHTTPRequest(t *testing.T) {
	client := NewClient(WithBaseURL("http://example.com"))
	req, err := client.R().
		SetBearerToken("token-1").
		SetURL("/api/:id").
		SetPathParam("id", "42").
		SetQueryParam("q", "go").
		SetJSONBody(map[string]string{"name": "gk"}).
		BuildHTTPRequest()
	if err != nil {
		t.Fatalf("build request failed: %v", err)
	}

	if req.Method != http.MethodGet {
		t.Fatalf("unexpected method %s", req.Method)
	}
	if req.URL.String() != "http://example.com/api/42?q=go" {
		t.Fatalf("unexpected URL %s", req.URL.String())
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token-1" {
		t.Fatalf("unexpected authorization %q", got)
	}
	if ct := req.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("unexpected content type %q", ct)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if !strings.Contains(string(body), `"name":"gk"`) {
		t.Fatalf("unexpected body %s", string(body))
	}
}

func TestRequestFromHTTPRequest(t *testing.T) {
	httpReq, err := http.NewRequest(http.MethodPost, "http://example.com/api?q=go", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("new http request: %v", err)
	}
	httpReq.Header.Set("X-Test", "1")
	httpReq.AddCookie(&http.Cookie{Name: "session", Value: "abc"})

	req := newRequest(nil).FromHTTPRequest(httpReq)
	if req.Method != http.MethodPost {
		t.Fatalf("unexpected method %s", req.Method)
	}
	if req.URL != "http://example.com/api" {
		t.Fatalf("unexpected url %s", req.URL)
	}
	if req.Header.Get("X-Test") != "1" {
		t.Fatalf("unexpected header")
	}
	if req.QueryParams.Get("q") != "go" {
		t.Fatalf("unexpected query params")
	}
	if len(req.Cookies) != 1 || req.Cookies[0].Value != "abc" {
		t.Fatalf("unexpected cookies %+v", req.Cookies)
	}
}

func TestRequestMustBuildHTTPRequest(t *testing.T) {
	req := NewClient(WithBaseURL("http://example.com")).R().
		SetURL("/must").
		SetMethod(http.MethodGet).
		MustBuildHTTPRequest()
	if req.URL.String() != "http://example.com/must" {
		t.Fatalf("unexpected url %s", req.URL.String())
	}
}

func TestRequestXMLAndPlainBodyHelpers(t *testing.T) {
	client := NewClient(WithBaseURL("http://example.com"))

	xmlReq, err := client.R().
		SetURL("/xml").
		SetXMLBody(xmlPayload{Name: "gk"}).
		BuildHTTPRequest()
	if err != nil {
		t.Fatalf("build xml request failed: %v", err)
	}
	if ct := xmlReq.Header.Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Fatalf("unexpected xml content type %q", ct)
	}

	plainReq, err := client.R().
		SetURL("/plain").
		SetPlainBody("hello").
		BuildHTTPRequest()
	if err != nil {
		t.Fatalf("build plain request failed: %v", err)
	}
	body, err := io.ReadAll(plainReq.Body)
	if err != nil {
		t.Fatalf("read plain body failed: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("unexpected plain body %q", string(body))
	}
}

func TestRequestRawRequestAssignedOnBuild(t *testing.T) {
	client := NewClient(WithBaseURL("http://example.com"))
	request := client.R().SetURL("/raw").SetQueryParamsFromValues(url.Values{"q": []string{"1"}})
	if _, err := request.BuildHTTPRequest(); err != nil {
		t.Fatalf("build request failed: %v", err)
	}
	if request.RawRequest == nil {
		t.Fatalf("expected raw request")
	}
}

func TestRequestValueAndAliasHelpers(t *testing.T) {
	req, err := NewClient(WithBaseURL("http://example.com")).R().
		SetURL("/values").
		SetHeaderValues(map[string][]string{"X-Test": {"1", "2"}}).
		SetUserAgent("ua-1").
		SetAccept("application/json").
		AddQueryValues(url.Values{"tag": []string{"go", "http"}}).
		AddFormValues(url.Values{"name": []string{"gk"}}).
		SetBytesBody([]byte("payload")).
		BuildHTTPRequest()
	if err != nil {
		t.Fatalf("build request failed: %v", err)
	}
	if got := req.Header.Values("X-Test"); len(got) != 2 {
		t.Fatalf("unexpected header values %+v", got)
	}
	if req.Header.Get("User-Agent") != "ua-1" {
		t.Fatalf("unexpected user agent")
	}
	if req.URL.Query().Get("tag") != "go" {
		t.Fatalf("unexpected query %s", req.URL.RawQuery)
	}
}

func TestRequestDumpAndCURL(t *testing.T) {
	request := NewClient(WithBaseURL("http://example.com")).R().
		SetMethod(http.MethodPost).
		SetURL("/dump").
		SetHeader("X-Test", "1").
		SetJSONBody(map[string]string{"name": "gk"})

	dump, err := request.Dump()
	if err != nil {
		t.Fatalf("dump request failed: %v", err)
	}
	if !strings.Contains(dump, "POST http://example.com/dump") {
		t.Fatalf("unexpected dump %q", dump)
	}

	curl, err := request.CURL()
	if err != nil {
		t.Fatalf("curl request failed: %v", err)
	}
	if !strings.Contains(curl, "curl -X POST") || !strings.Contains(curl, "http://example.com/dump") {
		t.Fatalf("unexpected curl %q", curl)
	}
}

func TestRequestDownload(t *testing.T) {
	target := filepath.Join(t.TempDir(), "download.txt")
	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("payload"))
		}))),
	)

	if err := client.R().SetURL("/download").Download(target); err != nil {
		t.Fatalf("download failed: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("unexpected payload %q", string(data))
	}
}

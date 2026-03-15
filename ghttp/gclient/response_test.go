package gclient

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResponseHelpers(t *testing.T) {
	resp := &Response{
		StatusCode: 201,
		Header:     http.Header{"X-Test": []string{"ok"}},
		Body:       []byte("hello"),
	}
	if !resp.IsSuccess() {
		t.Fatalf("expected success for 201")
	}
	if resp.HeaderGet("X-Test") != "ok" {
		t.Fatalf("unexpected header lookup")
	}
	if resp.String() != "hello" {
		t.Fatalf("unexpected body string")
	}
	if resp.Len() != 5 {
		t.Fatalf("unexpected body len %d", resp.Len())
	}
	if resp.StatusText() != "Created" {
		t.Fatalf("unexpected status text %q", resp.StatusText())
	}
}

func TestResponseToHTTPResponse(t *testing.T) {
	resp := &Response{
		StatusCode: 200,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       []byte("hello"),
	}

	httpResp := resp.ToHTTPResponse()
	if httpResp == nil {
		t.Fatalf("expected http response")
	}
	if httpResp.StatusCode != 200 {
		t.Fatalf("unexpected status code %d", httpResp.StatusCode)
	}
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("unexpected body %q", string(body))
	}
}

func TestResponseHelpersIntoAndFromHTTP(t *testing.T) {
	httpResp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"Set-Cookie":   []string{"session=abc; Path=/"},
		},
		Body: io.NopCloser(strings.NewReader(`{"name":"gk"}`)),
	}

	resp, err := ResponseFromHTTPResponse(httpResp)
	if err != nil {
		t.Fatalf("response from http response failed: %v", err)
	}

	var out struct {
		Name string `json:"name"`
	}
	if err := resp.Into(&out); err != nil {
		t.Fatalf("into failed: %v", err)
	}
	if out.Name != "gk" {
		t.Fatalf("unexpected decode result %+v", out)
	}
	rawBody, err := io.ReadAll(resp.RawResponse.Body)
	if err != nil {
		t.Fatalf("read raw response body failed: %v", err)
	}
	if string(rawBody) != `{"name":"gk"}` {
		t.Fatalf("unexpected raw response body %q", string(rawBody))
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "session" {
		t.Fatalf("unexpected cookies %+v", cookies)
	}
}

func TestResponseMustHelpers(t *testing.T) {
	resp := &Response{
		ContentType: "application/json",
		Body:        []byte(`{"name":"gk"}`),
	}
	var out struct {
		Name string `json:"name"`
	}
	resp.MustInto(&out)
	if out.Name != "gk" {
		t.Fatalf("unexpected decode result %+v", out)
	}
}

func TestResponseDumpAndSave(t *testing.T) {
	resp := &Response{
		StatusCode: 200,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       []byte("hello"),
	}

	dump := resp.Dump()
	if !strings.Contains(dump, "HTTP/1.1 200 OK") {
		t.Fatalf("unexpected dump %q", dump)
	}
	if resp.Size() != 5 {
		t.Fatalf("unexpected size %d", resp.Size())
	}
	if body, err := io.ReadAll(resp.Reader()); err != nil || string(body) != "hello" {
		t.Fatalf("unexpected reader body %q err=%v", string(body), err)
	}

	target := filepath.Join(t.TempDir(), "resp.txt")
	if err := resp.Save(target); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read saved file failed: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected saved body %q", string(data))
	}
}

func TestResponseCookiesFallbackAndToHTTPResponseStatus(t *testing.T) {
	resp := &Response{
		StatusCode: 204,
		Proto:      "HTTP/1.1",
		Header:     http.Header{"Set-Cookie": []string{"token=1; Path=/"}},
		Body:       []byte{},
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "token" {
		t.Fatalf("unexpected cookies %+v", cookies)
	}

	httpResp := resp.ToHTTPResponse()
	if httpResp.Status != "204 No Content" {
		t.Fatalf("unexpected status %q", httpResp.Status)
	}
}

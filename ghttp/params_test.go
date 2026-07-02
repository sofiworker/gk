package ghttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestParamsSnapshotAccessors(t *testing.T) {
	path := map[string]string{"name": "alice"}
	query := url.Values{
		"role": []string{"admin"},
		"tag":  []string{"a", "b"},
	}
	header := http.Header{
		"X-Token": []string{"abc"},
		"X-Tag":   []string{"h1", "h2"},
	}
	cookies := []*http.Cookie{
		{Name: "session_id", Value: "s1"},
		{Name: "theme", Value: "dark"},
	}

	params := newParams(path, query, header, cookies, "127.0.0.1")
	path["name"] = "bob"
	query["tag"][0] = "changed"
	header["X-Tag"][0] = "changed"
	cookies[0].Value = "changed"

	if got := params.Path("name"); got != "alice" {
		t.Fatalf("Path = %q, want alice", got)
	}
	if got := params.DefaultPath("missing", "fallback"); got != "fallback" {
		t.Fatalf("DefaultPath = %q, want fallback", got)
	}
	if got := params.Query("role"); got != "admin" {
		t.Fatalf("Query = %q, want admin", got)
	}
	if got := params.DefaultQuery("missing", "guest"); got != "guest" {
		t.Fatalf("DefaultQuery = %q, want guest", got)
	}
	if got := params.Header("X-Token"); got != "abc" {
		t.Fatalf("Header = %q, want abc", got)
	}
	if got := params.DefaultHeader("missing", "fallback"); got != "fallback" {
		t.Fatalf("DefaultHeader = %q, want fallback", got)
	}
	if got := params.Cookie("session_id"); got != "s1" {
		t.Fatalf("Cookie = %q, want s1", got)
	}
	if got := params.DefaultCookie("missing", "fallback"); got != "fallback" {
		t.Fatalf("DefaultCookie = %q, want fallback", got)
	}
	if got := params.ClientIP(); got != "127.0.0.1" {
		t.Fatalf("ClientIP = %q, want 127.0.0.1", got)
	}

	tags := params.QueryList("tag")
	if !reflect.DeepEqual(tags, []string{"a", "b"}) {
		t.Fatalf("QueryList = %#v, want a,b", tags)
	}
	tags[0] = "mutated"
	if got := params.QueryList("tag"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("QueryList after mutation = %#v, want original values", got)
	}
	headerTags := params.HeaderList("X-Tag")
	if !reflect.DeepEqual(headerTags, []string{"h1", "h2"}) {
		t.Fatalf("HeaderList = %#v, want h1,h2", headerTags)
	}
	headerTags[0] = "mutated"
	if got := params.HeaderList("X-Tag"); !reflect.DeepEqual(got, []string{"h1", "h2"}) {
		t.Fatalf("HeaderList after mutation = %#v, want original values", got)
	}
	snapshotCookies := params.Cookies()
	if len(snapshotCookies) != 2 || snapshotCookies[0].Value != "s1" || snapshotCookies[1].Value != "dark" {
		t.Fatalf("Cookies = %#v, want snapshot", snapshotCookies)
	}
	snapshotCookies[0].Value = "mutated"
	if got := params.Cookie("session_id"); got != "s1" {
		t.Fatalf("Cookie after mutation = %q, want s1", got)
	}
}

func TestParamsFromRequestCapturesCookies(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "abc"})
	req.AddCookie(&http.Cookie{Name: "locale", Value: "zh-CN"})

	params := paramsFromRequest(req, nil)

	if got := params.Cookie("session_id"); got != "abc" {
		t.Fatalf("Cookie(session_id) = %q, want abc", got)
	}
	if got := params.DefaultCookie("missing", "fallback"); got != "fallback" {
		t.Fatalf("DefaultCookie = %q, want fallback", got)
	}
}

func TestDefaultClientIPResolverUsesRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")

	if got := defaultClientIPResolver(req); got != "192.0.2.1" {
		t.Fatalf("defaultClientIPResolver = %q, want remote host", got)
	}
}

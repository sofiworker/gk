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

func TestParamsZeroValueIsReadSafe(t *testing.T) {
	var p Params
	if got := p.Path("id"); got != "" {
		t.Fatalf("Path = %q, want empty", got)
	}
	if got := p.Query("q"); got != "" {
		t.Fatalf("Query = %q, want empty", got)
	}
	if got := p.QueryList("q"); got != nil {
		t.Fatalf("QueryList = %#v, want nil", got)
	}
	if got := p.Header("X-Token"); got != "" {
		t.Fatalf("Header = %q, want empty", got)
	}
	if got := p.HeaderList("X-Token"); got != nil {
		t.Fatalf("HeaderList = %#v, want nil", got)
	}
	if got := p.Cookie("session_id"); got != "" {
		t.Fatalf("Cookie = %q, want empty", got)
	}
	if got := p.Cookies(); got != nil {
		t.Fatalf("Cookies = %#v, want nil", got)
	}
	if got := p.ClientIP(); got != "" {
		t.Fatalf("ClientIP = %q, want empty", got)
	}
	detached := p.Detach()
	if got := detached.Query("q"); got != "" {
		t.Fatalf("Detach().Query = %q, want empty", got)
	}
}

func TestParamsViewIsLazyAndCached(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/42?page=1&sort=asc", nil)
	req.Header.Set("Authorization", "token-1")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "s1"})
	req.RemoteAddr = "192.0.2.7:9999"

	params := paramsFromRequest(req, nil)

	if got := params.Query("page"); got != "1" {
		t.Fatalf("Query(page) = %q, want 1", got)
	}
	// The first access parses and caches; later URL mutation is not observed.
	req.URL.RawQuery = "page=999"
	if got := params.Query("page"); got != "1" {
		t.Fatalf("Query(page) after RawQuery mutation = %q, want cached 1", got)
	}

	if got := params.Cookie("session_id"); got != "s1" {
		t.Fatalf("Cookie = %q, want s1", got)
	}
	req.Header.Set("Cookie", "session_id=changed")
	if got := params.Cookie("session_id"); got != "s1" {
		t.Fatalf("Cookie after header mutation = %q, want cached s1", got)
	}

	if got := params.ClientIP(); got != "192.0.2.7" {
		t.Fatalf("ClientIP = %q, want 192.0.2.7", got)
	}
	req.RemoteAddr = "198.51.100.1:1"
	if got := params.ClientIP(); got != "192.0.2.7" {
		t.Fatalf("ClientIP after RemoteAddr mutation = %q, want cached", got)
	}

	// Headers read through to the live request (view semantics).
	req.Header.Set("Authorization", "token-2")
	if got := params.Header("Authorization"); got != "token-2" {
		t.Fatalf("Header = %q, want live token-2", got)
	}

	// Copies of the view share the lazily built caches.
	copied := params
	if got := copied.Query("sort"); got != "asc" {
		t.Fatalf("copied Query(sort) = %q, want asc", got)
	}
}

func TestParamsDetachSnapshotsAndDropsRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/42?page=1", nil)
	req.Header.Set("Authorization", "token-1")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "s1"})
	req.RemoteAddr = "192.0.2.7:9999"

	view := paramsFromRequest(req, nil)
	detached := view.Detach()

	if detached.state == nil || detached.state.req != nil {
		t.Fatalf("Detach must drop the request reference")
	}

	// Mutating the request after Detach must not affect the snapshot.
	req.Header.Set("Authorization", "token-2")
	req.URL.RawQuery = "page=999"
	req.Header.Set("Cookie", "session_id=changed")

	if got := detached.Header("Authorization"); got != "token-1" {
		t.Fatalf("detached Header = %q, want token-1", got)
	}
	if got := detached.Query("page"); got != "1" {
		t.Fatalf("detached Query = %q, want 1", got)
	}
	if got := detached.Cookie("session_id"); got != "s1" {
		t.Fatalf("detached Cookie = %q, want s1", got)
	}
	if got := detached.ClientIP(); got != "192.0.2.7" {
		t.Fatalf("detached ClientIP = %q, want 192.0.2.7", got)
	}
}

func TestParamsPathParamsFromRoute(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	var route pathParamList
	route.Add("id", "42")

	params := paramsFromRequestWithPathParams(req, nil, route)
	if got := params.Path("id"); got != "42" {
		t.Fatalf("Path = %q, want 42", got)
	}
	detached := params.Detach()
	if got := detached.Path("id"); got != "42" {
		t.Fatalf("detached Path = %q, want 42", got)
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

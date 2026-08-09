package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostValidationDisabledByDefault(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[map[string]string, map[string]string](app).GET("/ping").ToNoInput(func(ctx context.Context) (map[string]string, error) {
		return map[string]string{"ok": "true"}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.Host = "evil.example.com"
	app.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (host validation off by default)", w.Code)
	}
}

func TestAllowedHostsExactMatch(t *testing.T) {
	app := New(
		WithProduces(MIMEJSON),
		WithHostValidator(AllowedHosts("api.example.com")),
	)
	Route[map[string]string, map[string]string](app).GET("/ping").ToNoInput(func(ctx context.Context) (map[string]string, error) {
		return map[string]string{"ok": "true"}, nil
	})

	// 匹配放行
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.Host = "api.example.com"
	app.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("allowed host status = %d, want 200", w.Code)
	}

	// 不匹配拒绝（DNS rebinding 防护）
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.Host = "evil.example.com"
	app.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("rejected host status = %d, want 400", w.Code)
	}
}

func TestAllowedHostsWildcardSubdomain(t *testing.T) {
	v := AllowedHosts("*.example.com")

	if !v("foo.example.com") {
		t.Fatal("*.example.com should match foo.example.com")
	}
	if v("example.com") {
		t.Fatal("*.example.com should NOT match bare example.com")
	}
	if v("evil.org") {
		t.Fatal("*.example.com should not match evil.org")
	}
	// 端口剥离
	if !v("foo.example.com:8080") {
		t.Fatal("foo.example.com:8080 should match after port strip")
	}
}

func TestTrustedHostResolverReadsForwardedHostOnlyFromTrustedProxy(t *testing.T) {
	cidrs, _ := parseTrustedCIDRs([]string{"192.0.2.0/24"})
	resolver := TrustedHostResolver(cidrs, defaultForwardedHostHeaders)

	// 信任代理：读 X-Forwarded-Host
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Host = "internal.example.com"
	req.Header.Set("X-Forwarded-Host", "api.example.com")
	if got := resolver(req); got != "api.example.com" {
		t.Fatalf("host = %q, want forwarded api.example.com", got)
	}

	// 非信任来源：忽略 X-Forwarded-Host，回退 r.Host
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "203.0.113.5:1234"
	req2.Host = "internal.example.com"
	req2.Header.Set("X-Forwarded-Host", "evil.example.com")
	if got := resolver(req2); got != "internal.example.com" {
		t.Fatalf("host = %q, want direct Host (forwarded ignored)", got)
	}
}

func TestTrustedHostResolverNoForwardedHeaderFallsBack(t *testing.T) {
	cidrs, _ := parseTrustedCIDRs([]string{"0.0.0.0/0"})
	resolver := TrustedHostResolver(cidrs, defaultForwardedHostHeaders)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Host = "api.example.com"
	if got := resolver(req); got != "api.example.com" {
		t.Fatalf("host = %q, want direct Host when no forwarded header", got)
	}
}

func TestTrustedClientIPResolver(t *testing.T) {
	cidrs, _ := parseTrustedCIDRs([]string{"10.0.0.0/8"})
	resolver := TrustedClientIPResolver(cidrs, defaultForwardedIPHeaders)

	// 信任代理：取 X-Forwarded-For 首个 IP
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.5")
	if got := resolver(req); got != "203.0.113.9" {
		t.Fatalf("client IP = %q, want first forwarded 203.0.113.9", got)
	}

	// 非信任来源：回退 RemoteAddr
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "198.51.100.7:1234"
	req2.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := resolver(req2); got != "198.51.100.7" {
		t.Fatalf("client IP = %q, want RemoteAddr (forwarded ignored)", got)
	}
}

func TestParseTrustedCIDRsInvalid(t *testing.T) {
	if _, err := parseTrustedCIDRs([]string{"not-a-cidr"}); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestIsUnsafeTrustedProxies(t *testing.T) {
	all, _ := parseTrustedCIDRs([]string{"0.0.0.0/0"})
	if !isUnsafeTrustedProxies(all) {
		t.Fatal("0.0.0.0/0 should be unsafe")
	}

	restricted, _ := parseTrustedCIDRs([]string{"10.0.0.0/8"})
	if isUnsafeTrustedProxies(restricted) {
		t.Fatal("10.0.0.0/8 should be safe")
	}
}

func TestIsTrustedProxy(t *testing.T) {
	cidrs, _ := parseTrustedCIDRs([]string{"192.0.2.0/24"})

	if !isTrustedProxy("192.0.2.10:1234", cidrs) {
		t.Fatal("192.0.2.10 should be trusted")
	}
	if isTrustedProxy("203.0.113.5:1234", cidrs) {
		t.Fatal("203.0.113.5 should not be trusted")
	}
}

func TestWithTrustedProxiesInvalidPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid CIDR in WithTrustedProxies")
		}
	}()
	New(WithTrustedProxies("bad-cidr"))
}

func TestDefaultTrustsAllProxies(t *testing.T) {
	c := &Config{}
	// 未配置时默认信任所有 IP（New 会填充 defaultTrustedCIDRs）。
	New(WithProduces(MIMEJSON))
	if !isUnsafeTrustedProxies(defaultTrustedCIDRs) {
		t.Fatal("default trust boundary should cover all IPs")
	}
	_ = c
}

func TestAllowedHostsMatchesIPAndPort(t *testing.T) {
	v := AllowedHosts("192.168.1.10")
	if !v("192.168.1.10:8080") {
		t.Fatal("IP host with port should match")
	}
}

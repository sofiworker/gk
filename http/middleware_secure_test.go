package ghttp

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func secRun(t *testing.T, mw Middleware, r *http.Request, h Handler) *httptest.ResponseRecorder {
	t.Helper()
	if h == nil {
		h = func(context.Context, *Request, *Response) error { return nil }
	}
	rec := httptest.NewRecorder()
	if err := mw(h)(r.Context(), &Request{Raw: r}, &Response{Writer: rec}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return rec
}

func TestSecureHeadersDefaults(t *testing.T) {
	rec := secRun(t, SecureHeaders(), httptest.NewRequest("GET", "/", nil), nil)
	want := map[string]string{
		HeaderXContentTypeOptions:     "nosniff",
		HeaderXFrameOptions:           "DENY",
		HeaderReferrerPolicy:          "strict-origin-when-cross-origin",
		HeaderCrossOriginOpenerPolicy: "same-origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if rec.Header().Get(HeaderStrictTransportSecurity) != "" || rec.Header().Get(HeaderContentSecurityPolicy) != "" {
		t.Error("HSTS/CSP must not be set by default")
	}
}

func TestSecureHeadersOptions(t *testing.T) {
	mw := SecureHeaders(
		WithCSP("default-src 'self'"),
		WithFrameOptions("SAMEORIGIN"),
		WithReferrerPolicy("no-referrer"),
		WithoutHeader("cross-origin-opener-policy"),
		WithHSTS(365*24*time.Hour, true, true),
	)
	rec := secRun(t, mw, httptest.NewRequest("GET", "/", nil), nil)
	h := rec.Header()
	if h.Get(HeaderContentSecurityPolicy) != "default-src 'self'" || h.Get(HeaderXFrameOptions) != "SAMEORIGIN" ||
		h.Get(HeaderReferrerPolicy) != "no-referrer" {
		t.Errorf("headers = %v", h)
	}
	if _, ok := h[HeaderCrossOriginOpenerPolicy]; ok {
		t.Error("COOP should be removed")
	}
	if h.Get(HeaderStrictTransportSecurity) != "" {
		t.Error("HSTS must not be sent over plain HTTP")
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{}
	got := secRun(t, mw, r, nil).Header().Get(HeaderStrictTransportSecurity)
	if got != "max-age=31536000; includeSubDomains; preload" {
		t.Errorf("HSTS = %q", got)
	}
}

func TestSecureHeadersForwardedProto(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if secRun(t, SecureHeaders(WithHSTS(time.Hour, false, false)), r, nil).Header().Get(HeaderStrictTransportSecurity) != "" {
		t.Error("untrusted X-Forwarded-Proto must be ignored")
	}
	rec := secRun(t, SecureHeaders(WithHSTS(time.Hour, false, false), WithSecureTrustForwardedProto()), r, nil)
	if got := rec.Header().Get(HeaderStrictTransportSecurity); got != "max-age=3600" {
		t.Errorf("HSTS = %q", got)
	}
}

func TestSecureHeadersHandlerOverride(t *testing.T) {
	h := func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set(HeaderXFrameOptions, "SAMEORIGIN")
		return nil
	}
	rec := secRun(t, SecureHeaders(), httptest.NewRequest("GET", "/", nil), h)
	if rec.Header().Get(HeaderXFrameOptions) != "SAMEORIGIN" {
		t.Error("handler override lost")
	}
}

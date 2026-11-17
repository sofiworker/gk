package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cipRun(t *testing.T, remote string, hdr map[string]string, opts ...RealIPOption) string {
	t.Helper()
	mw, err := RealIP(opts...)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	req := &Request{Raw: r}
	var got string
	h := mw(func(ctx context.Context, req *Request, _ *Response) error {
		got = ClientIP(req)
		return nil
	})
	if err := h(r.Context(), req, &Response{Writer: httptest.NewRecorder()}); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestClientIP_RealIP(t *testing.T) {
	trust := WithTrustedProxies("10.0.0.0/8", "192.168.1.1", "fd00::/8")
	tests := []struct {
		name   string
		remote string
		hdr    map[string]string
		opts   []RealIPOption
		want   string
	}{
		{"untrusted peer ignores forged XFF", "203.0.113.9:1234", map[string]string{"X-Forwarded-For": "1.2.3.4"}, []RealIPOption{trust}, "203.0.113.9"},
		{"no trusted configured", "10.0.0.1:1", map[string]string{"X-Forwarded-For": "1.2.3.4"}, nil, "10.0.0.1"},
		{"multi hop", "10.0.0.1:1", map[string]string{"X-Forwarded-For": "6.6.6.6, 1.2.3.4, 10.0.0.5"}, []RealIPOption{trust}, "1.2.3.4"},
		{"single ip trusted", "192.168.1.1:80", map[string]string{"X-Forwarded-For": "8.8.8.8"}, []RealIPOption{trust}, "8.8.8.8"},
		{"ipv6 peer and xff", "[fd00::1]:99", map[string]string{"X-Forwarded-For": "2001:db8::5, fd00::2"}, []RealIPOption{trust}, "2001:db8::5"},
		{"ipv6 untrusted", "[2001:db8::1]:99", nil, []RealIPOption{trust}, "2001:db8::1"},
		{"invalid xff item", "10.0.0.1:1", map[string]string{"X-Forwarded-For": "1.2.3.4, garbage"}, []RealIPOption{trust}, "10.0.0.1"},
		{"no xff", "10.0.0.1:1", nil, []RealIPOption{trust}, "10.0.0.1"},
		{"all trusted", "10.0.0.1:1", map[string]string{"X-Forwarded-For": "10.0.0.2, 10.0.0.3"}, []RealIPOption{trust}, "10.0.0.1"},
		{"x-real-ip", "10.0.0.1:1", map[string]string{"X-Real-IP": "9.9.9.9"}, []RealIPOption{trust, WithRealIPHeader("X-Real-IP")}, "9.9.9.9"},
		{"x-real-ip untrusted peer", "1.1.1.1:1", map[string]string{"X-Real-IP": "9.9.9.9"}, []RealIPOption{trust, WithRealIPHeader("X-Real-IP")}, "1.1.1.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cipRun(t, tc.remote, tc.hdr, tc.opts...); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestClientIP_Fallback(t *testing.T) {
	for remote, want := range map[string]string{"1.2.3.4:80": "1.2.3.4", "[::1]:80": "::1", "5.6.7.8": "5.6.7.8", "[::2]": "::2"} {
		req := &Request{Raw: (&http.Request{RemoteAddr: remote, Header: http.Header{}}).WithContext(context.Background())}
		if got := ClientIP(req); got != want {
			t.Errorf("%s: got %q want %q", remote, got, want)
		}
	}
	if ClientIP(nil) != "" {
		t.Error("nil request")
	}
}

func TestClientIP_InvalidTrustedProxy(t *testing.T) {
	_, err := RealIP(WithTrustedProxies("10.0.0.0/8", "nope"))
	if !errors.Is(err, ErrInvalidTrustedProxy) {
		t.Fatalf("err = %v", err)
	}
}

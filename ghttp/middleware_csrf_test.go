package ghttp

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func csrfCall(t *testing.T, mw Middleware, method string, hdr map[string]string) (called bool, err error) {
	t.Helper()
	r := httptest.NewRequest(method, "http://app.example.com/x", nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	h := mw(func(context.Context, *Request, *Response) error { called = true; return nil })
	err = h(r.Context(), &Request{Raw: r}, &Response{Writer: httptest.NewRecorder()})
	return
}

func TestCSRF(t *testing.T) {
	mw, err := CSRF(WithCSRFTrustedOrigins("https://trusted.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		method string
		hdr    map[string]string
		allow  bool
	}{
		{"GET cross-site", "GET", map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{"GET cross origin", "GET", map[string]string{"Origin": "https://evil.com"}, true},
		{"POST no headers", "POST", nil, true},
		{"POST same-origin", "POST", map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"POST none", "POST", map[string]string{"Sec-Fetch-Site": "none"}, true},
		{"POST same Origin host", "POST", map[string]string{"Origin": "http://app.example.com"}, true},
		{"POST cross-site", "POST", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"POST same-site", "POST", map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		{"POST cross Origin", "POST", map[string]string{"Origin": "https://evil.com"}, false},
		{"POST trusted origin", "POST", map[string]string{"Origin": "https://trusted.example.com"}, true},
		{"DELETE cross Origin", "DELETE", map[string]string{"Origin": "https://evil.com"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			called, err := csrfCall(t, mw, c.method, c.hdr)
			if c.allow {
				if err != nil || !called {
					t.Fatalf("want allowed, called=%v err=%v", called, err)
				}
				return
			}
			if called || err == nil {
				t.Fatalf("want rejected, called=%v err=%v", called, err)
			}
			if !errors.Is(err, ErrForbidden) {
				t.Errorf("err should match ErrForbidden: %v", err)
			}
			if !errors.Is(err, ErrCrossOriginRequest) {
				t.Errorf("cause lost: %v", err)
			}
		})
	}
}

func TestCSRFInvalidTrustedOrigin(t *testing.T) {
	if _, err := CSRF(WithCSRFTrustedOrigins("not-an-origin")); err == nil {
		t.Fatal("expected construction error")
	}
}

func TestCSRFBypassFunc(t *testing.T) {
	mw, err := CSRF(WithCSRFBypassFunc(func(r *Request) bool { return r.Raw.URL.Path == "/x" }))
	if err != nil {
		t.Fatal(err)
	}
	called, err := csrfCall(t, mw, "POST", map[string]string{"Sec-Fetch-Site": "cross-site"})
	if err != nil || !called {
		t.Fatalf("bypass failed: %v", err)
	}
}

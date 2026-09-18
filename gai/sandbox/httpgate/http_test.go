package httpgate_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	s "github.com/sofiworker/gk/gai/sandbox"
	"github.com/sofiworker/gk/gai/sandbox/httpgate"
)

func rule(t *testing.T, raw string) httpgate.Rule {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return httpgate.Rule{Scheme: u.Scheme, Host: u.Hostname(), Port: uint16(port), Methods: []string{"GET", "POST"}, AllowPrivate: true}
}
func client(t *testing.T, options ...httpgate.Option) *httpgate.Client {
	t.Helper()
	c, err := httpgate.New(options...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestHTTPPolicyCredentialRedirectAndLimits(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credential crossed destination")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "secret" {
			t.Error("missing injected credential")
		}
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer origin.Close()
	first := rule(t, origin.URL)
	first.Headers = http.Header{"X-Key": []string{"secret"}}
	c := client(t, httpgate.WithRules(first, rule(t, destination.URL)), httpgate.WithRedirects(1))
	out, err := c.Send(context.Background(), s.HTTPRequest{Method: "GET", URL: origin.URL, Header: http.Header{"Authorization": []string{"caller"}, "Cookie": []string{"private"}}})
	if err != nil || string(out.Body) != "ok" {
		t.Fatal(out, err)
	}
	denied := client(t, httpgate.WithRules(first, rule(t, destination.URL)))
	_, err = denied.Send(context.Background(), s.HTTPRequest{Method: "GET", URL: origin.URL})
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	private := rule(t, destination.URL)
	private.AllowPrivate = false
	_, err = client(t, httpgate.WithRules(private)).Send(context.Background(), s.HTTPRequest{Method: "GET", URL: destination.URL})
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	_, err = c.Send(context.Background(), s.HTTPRequest{Method: "DELETE", URL: origin.URL})
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	limited := client(t, httpgate.WithRules(rule(t, destination.URL)), httpgate.WithLimits(1, 1, 1, time.Second))
	_, err = limited.Send(context.Background(), s.HTTPRequest{Method: "POST", URL: destination.URL, Body: []byte("too big")})
	if !errors.Is(err, s.ErrQuota) {
		t.Fatal(err)
	}
	_, err = limited.Send(context.Background(), s.HTTPRequest{Method: "GET", URL: destination.URL})
	if !errors.Is(err, s.ErrQuota) {
		t.Fatal(err)
	}
}
func TestHTTPNoProxyAndCancellation(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		_, _ = fmt.Fprint(w, "direct")
	}))
	defer srv.Close()
	c := client(t, httpgate.WithRules(rule(t, srv.URL)))
	out, err := c.Send(context.Background(), s.HTTPRequest{Method: "GET", URL: srv.URL})
	if err != nil || string(out.Body) != "direct" {
		t.Fatal(out, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = c.Send(ctx, s.HTTPRequest{Method: "GET", URL: srv.URL + "/slow"})
	if err == nil {
		t.Fatal("missing cancellation")
	}
	for _, raw := range []string{strings.Replace(srv.URL, "http://", "http://user:password@", 1), srv.URL + "/#fragment", "file:///etc/passwd"} {
		_, err = c.Send(context.Background(), s.HTTPRequest{Method: "GET", URL: raw})
		if !errors.Is(err, s.ErrDenied) {
			t.Fatal(raw, err)
		}
	}
}
func TestHTTPInvalidOptions(t *testing.T) {
	for _, o := range []httpgate.Option{nil, httpgate.WithRules(httpgate.Rule{}), httpgate.WithLimits(0, 1, 1, time.Second), httpgate.WithRedirects(21), httpgate.WithResolver(nil)} {
		if _, err := httpgate.New(o); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
	if _, err := httpgate.New(httpgate.WithResolver(&net.Resolver{})); err != nil {
		t.Fatal(err)
	}
}

package httptransport_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sofiworker/gk/gai/transport/httptransport"
)

func TestTransportBoundaries(t *testing.T) {
	for _, mode := range []string{"redirect", "status", "large", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, "http://127.0.0.1:1", 302)
				case "status":
					w.WriteHeader(401)
				case "large":
					_, _ = w.Write(make([]byte, 2048))
				case "invalid":
					_, _ = w.Write([]byte("bad"))
				case "timeout":
					select {
					case <-r.Context().Done():
					case <-time.After(100 * time.Millisecond):
					}
				}
			}))
			defer server.Close()
			client, err := httptransport.New(httptransport.WithLimits(20*time.Millisecond, 1024))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			resp, e := client.Do(context.Background(), httptransport.Request{Method: "POST", URL: server.URL, Body: []byte("{}")})
			err = e
			if mode == "redirect" || mode == "status" {
				if err != nil || resp.StatusCode < 300 {
					t.Fatal(resp, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected failure")
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if mode == "large" && !errors.Is(err, httptransport.ErrLimit) {
				t.Fatal(err)
			}
		})
	}
	client, err := httptransport.New()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err = client.Do(context.Background(), httptransport.Request{Method: "POST", URL: "file:///etc/passwd"}); err == nil {
		t.Fatal("invalid endpoint")
	}
}

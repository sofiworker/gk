package rawprobe

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestRunEmpty(t *testing.T) {
	r := Run(t.Context(), "127.0.0.1:1", []Case{{Name: "x", Payload: nil}})
	if r.Passed+r.Failed != 1 {
		t.Fatal(r)
	}
}

func fixture(t *testing.T, response []byte, hold bool) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 4096)
		_, _ = c.Read(b)
		_, _ = c.Write(response)
		if hold {
			time.Sleep(5 * time.Second)
		}
	}()
	return l.Addr().String()
}

func TestRunListenerResponses(t *testing.T) {
	payload := []byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	t.Run("valid", func(t *testing.T) {
		// Keep the connection open so non-ExpectClose validates a complete response without requiring EOF.
		addr := fixture(t, []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"), true)
		r := Run(context.Background(), addr, []Case{{Name: "ok", Payload: payload, AllowedStatus: []int{200}}})
		if !r.Results[0].Healthy {
			t.Fatal(r.Results[0])
		}
	})
	t.Run("truncated", func(t *testing.T) {
		addr := fixture(t, []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nxy"), false)
		r := Run(context.Background(), addr, []Case{{Name: "short", Payload: payload, AllowedStatus: []int{200}}})
		if r.Results[0].Class != "truncated" {
			t.Fatal(r.Results[0])
		}
	})
	t.Run("invalid-status", func(t *testing.T) {
		addr := fixture(t, []byte("HTTP/1.1 700 Nope\r\nContent-Length: 0\r\n\r\n"), false)
		r := Run(context.Background(), addr, []Case{{Name: "bad", Payload: payload}})
		if r.Results[0].Healthy {
			t.Fatal(r.Results[0])
		}
	})
	t.Run("expect-close", func(t *testing.T) {
		addr := fixture(t, []byte("HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Length: 0\r\n\r\n"), false)
		r := Run(context.Background(), addr, []Case{{Name: "close", Payload: payload, AllowedStatus: []int{200}, ExpectClose: true}})
		if !r.Results[0].Healthy {
			t.Fatal(r.Results[0])
		}
	})
	t.Run("limit", func(t *testing.T) {
		addr := fixture(t, []byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nabcd"), false)
		r := Run(context.Background(), addr, []Case{{Name: "lim", Payload: payload, MaxResponseLen: 2}})
		if r.Results[0].Class != "response_limit" {
			t.Fatal(fmt.Sprint(r.Results[0]))
		}
	})
}

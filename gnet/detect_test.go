package gnet

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// TestMatchTLS 覆盖 TLS 首字节识别。
func TestMatchTLS(t *testing.T) {
	if !MatchTLS([]byte{0x16, 0x03, 0x01}) {
		t.Fatal("MatchTLS(0x16...) = false, want true")
	}
	if MatchTLS([]byte("GET /")) {
		t.Fatal("MatchTLS(GET) = true, want false")
	}
	if MatchTLS(nil) {
		t.Fatal("MatchTLS(nil) = true, want false")
	}
}

// TestMatchHTTP 覆盖 HTTP 方法前缀识别。
func TestMatchHTTP(t *testing.T) {
	for _, m := range []string{"GET /", "POST /x", "PUT ", "DELETE ", "HEAD ", "OPTIONS ", "PATCH ", "CONNECT ", "TRACE "} {
		if !MatchHTTP([]byte(m)) {
			t.Fatalf("MatchHTTP(%q) = false, want true", m)
		}
	}
	if MatchHTTP([]byte("HTTP/1.1")) || MatchHTTP([]byte("TLS")) {
		t.Fatal("MatchHTTP matched non-HTTP prefix")
	}
}

// dialPipe 建立内存连接并预写数据（写完即关，保证对端可读到 EOF）。
func dialPipe(t *testing.T, payload string) net.Conn {
	t.Helper()
	srv, cli := net.Pipe()
	go func() {
		_, _ = io.WriteString(srv, payload)
		_ = srv.Close()
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = cli.Close()
	})
	return cli
}

// TestDetectProtocolHTTP 覆盖 HTTP 识别与字节回灌。
func TestDetectProtocolHTTP(t *testing.T) {
	conn := dialPipe(t, "GET /hello HTTP/1.1\r\nHost: x\r\n\r\n")
	matchers := []NamedMatcher{
		{Name: "tls", Match: MatchTLS},
		{Name: "http", Match: MatchHTTP},
	}
	name, upgraded, err := DetectProtocol(conn, 8, matchers)
	if err != nil {
		t.Fatalf("DetectProtocol: %v", err)
	}
	if name != "http" {
		t.Fatalf("name = %q, want http", name)
	}
	// 回灌：升级连接必须能读到完整请求。
	_ = upgraded.SetReadDeadline(time.Now().Add(2 * time.Second))
	full, err := io.ReadAll(upgraded)
	if err != nil {
		t.Fatalf("read upgraded: %v", err)
	}
	if !strings.HasPrefix(string(full), "GET /hello") {
		t.Fatalf("replayed data = %q", full)
	}
}

// TestDetectProtocolNoMatch 覆盖未匹配路径：连接仍可用。
func TestDetectProtocolNoMatch(t *testing.T) {
	conn := dialPipe(t, "hello world")
	_, upgraded, err := DetectProtocol(conn, 4, []NamedMatcher{{Name: "tls", Match: MatchTLS}})
	if !errors.Is(err, ErrNoProtocolMatch) {
		t.Fatalf("err = %v, want ErrNoProtocolMatch", err)
	}
	_ = upgraded.SetReadDeadline(time.Now().Add(2 * time.Second))
	full, err := io.ReadAll(upgraded)
	if err != nil {
		t.Fatalf("read upgraded: %v", err)
	}
	if string(full) != "hello world" {
		t.Fatalf("replayed data = %q, want hello world", full)
	}
}

// TestDetectProtocolTLS 覆盖 TLS 识别。
func TestDetectProtocolTLS(t *testing.T) {
	conn := dialPipe(t, "\x16\x03\x01\x00\x05garbage")
	name, _, err := DetectProtocol(conn, 3, []NamedMatcher{{Name: "tls", Match: MatchTLS}})
	if err != nil {
		t.Fatalf("DetectProtocol: %v", err)
	}
	if name != "tls" {
		t.Fatalf("name = %q, want tls", name)
	}
}

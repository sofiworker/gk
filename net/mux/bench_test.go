package mux

import (
	"context"
	"io"
	"net"
	"testing"
)

// sessionPairB 是基准用会话对（TCP 回环）。
func sessionPairB(b *testing.B) (*Session, *Session) {
	b.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = ln.Close() })
	accCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accCh <- c
		}
	}()
	cc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	sc := <-accCh
	client, err := New(cc)
	if err != nil {
		b.Fatal(err)
	}
	server, err := New(sc)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client, server
}

// BenchmarkMuxStream 基准：单流 4KB 写读往返（复用会话）。
func BenchmarkMuxStream(b *testing.B) {
	client, server := sessionPairB(b)
	go func() {
		st, err := server.AcceptStream(context.Background())
		if err != nil {
			return
		}
		defer st.Close()
		_, _ = io.Copy(st, st) // echo
	}()
	st, err := client.OpenStream(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i)
	}
	buf := make([]byte, len(payload))
	b.SetBytes(int64(len(payload) * 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.Write(payload); err != nil {
			b.Fatal(err)
		}
		if _, err := io.ReadFull(st, buf); err != nil {
			b.Fatal(err)
		}
	}
}

package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func TestRunRejectsArguments(t *testing.T) {
	for _, args := range [][]string{{"-bad"}, {"-max-body-bytes", "0"}, {"-addr", "bad address"}} {
		if err := run(t.Context(), args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("run(%v) succeeded", args)
		}
	}
}

func TestRunReadyHealthAndGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out lockedBuffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-shutdown-timeout", "2s"}, &out, &bytes.Buffer{})
	}()
	var url string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line := strings.TrimSpace(out.String())
		if strings.HasPrefix(line, "READY ") {
			url = strings.TrimPrefix(line, "READY ")
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if url == "" {
		t.Fatalf("ready output = %q", out.String())
	}
	resp, err := http.Get(url + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestRunShutdownFileTriggersGracefulShutdown(t *testing.T) {
	shutdownFile := filepath.Join(t.TempDir(), "shutdown")
	var out lockedBuffer
	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), []string{"-addr", "127.0.0.1:0", "-shutdown-file", shutdownFile}, &out, &bytes.Buffer{})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.HasPrefix(strings.TrimSpace(out.String()), "READY ") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(shutdownFile, []byte("shutdown"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown file did not stop server")
	}
}

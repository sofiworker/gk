package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestLifecycleReadyInflightAndShutdown(t *testing.T) {
	running := startLifecycleServer(t)

	response, err := running.client.Get(running.url + "/ready")
	if err != nil {
		t.Fatalf("GET /ready error = %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /ready status = %d, want 200", response.StatusCode)
	}

	requestDone := make(chan error, 1)
	go func() {
		response, err := running.client.Get(running.url + "/fault/delay?ms=75")
		if err != nil {
			requestDone <- err
			return
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		requestDone <- readErr
	}()
	// 指标端点自排除后,快照只包含在途的 /fault/delay 请求。
	// With the metrics endpoint self-excluded, the snapshot only includes the
	// in-flight /fault/delay request.
	waitForActiveRequests(t, running, 1)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := running.server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-requestDone; err != nil {
		t.Fatalf("in-flight request error = %v", err)
	}
	if err := <-running.serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() error = %v, want ErrServerClosed", err)
	}
	assertNewConnectionFails(t, running)
}

type lifecycleServer struct {
	server    *http.Server
	client    *http.Client
	url       string
	serveDone chan error
	serveExit chan struct{}
}

func startLifecycleServer(t *testing.T) *lifecycleServer {
	t.Helper()
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	serveDone := make(chan error, 1)
	serveExit := make(chan struct{})
	go func() {
		defer close(serveExit)
		serveDone <- server.Serve(listener)
	}()
	running := &lifecycleServer{
		server:    server,
		client:    &http.Client{Timeout: 5 * time.Second},
		url:       "http://" + listener.Addr().String(),
		serveDone: serveDone,
		serveExit: serveExit,
	}
	t.Cleanup(func() {
		_ = server.Close()
		select {
		case <-serveExit:
		case <-time.After(2 * time.Second):
			t.Error("Serve goroutine did not exit during cleanup")
		}
	})
	return running
}

func waitForActiveRequests(t *testing.T, running *lifecycleServer, minimum uint64) {
	t.Helper()
	waitForLifecycleMetric(t, running, func(snapshot MetricsSnapshot) bool {
		return snapshot.ActiveRequests >= minimum
	})
}

func waitForLifecycleMetric(t *testing.T, running *lifecycleServer, ready func(MetricsSnapshot) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, running.url+"/__test/metrics", nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext() error = %v", err)
		}
		response, err := running.client.Do(request)
		if err == nil {
			var snapshot MetricsSnapshot
			decodeErr := json.NewDecoder(response.Body).Decode(&snapshot)
			_ = response.Body.Close()
			if decodeErr == nil && ready(snapshot) {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("metric condition not reached: %v", ctx.Err())
		default:
		}
	}
}

func assertNewConnectionFails(t *testing.T, running *lifecycleServer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, running.url+"/ready", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	if response, err := running.client.Do(request); err == nil {
		_ = response.Body.Close()
		t.Fatal("request after shutdown succeeded")
	}
}

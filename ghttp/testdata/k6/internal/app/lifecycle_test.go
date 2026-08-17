package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sofiworker/gk/ghttp"
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
	waitForActiveRequests(t, running, 2)

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

func TestShutdownWithStreamingConnectionsIsControlled(t *testing.T) {
	running := startStreamingLifecycleServer(t)

	sseCtx, cancelSSE := context.WithCancel(context.Background())
	sseRequest, err := http.NewRequestWithContext(sseCtx, http.MethodGet, running.url+"/sse/slow?interval_ms=1000&count=100", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	sseResponse, err := running.client.Do(sseRequest)
	if err != nil {
		t.Fatalf("SSE Do() error = %v", err)
	}
	if line, err := bufio.NewReader(sseResponse.Body).ReadString('\n'); err != nil || line != "id: 1\n" {
		t.Fatalf("SSE first line = %q, error = %v", line, err)
	}

	ws := dialLifecycleWebSocket(t, running.url+"/ws/echo")
	waitForLifecycleConnections(t, running, 1, 1)

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err = running.server.Shutdown(shortCtx)
	shortCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("short Shutdown() error = %v, want DeadlineExceeded", err)
	}

	cancelSSE()
	_ = sseResponse.Body.Close()
	_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "test complete"))
	_ = ws.Close()
	waitForRuntimeConnections(t, running.metrics, 0, 0)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := running.server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	if err := <-running.serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() error = %v, want ErrServerClosed", err)
	}
	assertNewConnectionFails(t, running)
}

func TestShutdownNilContextPanicsLikeStandardLibrary(t *testing.T) {
	running := startStreamingLifecycleServer(t)
	sseCtx, cancelSSE := context.WithCancel(context.Background())
	sseRequest, err := http.NewRequestWithContext(sseCtx, http.MethodGet, running.url+"/sse/slow?interval_ms=1000&count=100", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	sseResponse, err := running.client.Do(sseRequest)
	if err != nil {
		t.Fatalf("SSE Do() error = %v", err)
	}
	if line, err := bufio.NewReader(sseResponse.Body).ReadString('\n'); err != nil || line != "id: 1\n" {
		t.Fatalf("SSE first line = %q, error = %v", line, err)
	}
	waitForRuntimeConnections(t, running.metrics, 1, 0)
	type shutdownResult struct {
		err       error
		recovered any
	}
	result := make(chan shutdownResult, 1)
	go func() {
		var outcome shutdownResult
		defer func() {
			outcome.recovered = recover()
			result <- outcome
		}()
		outcome.err = running.server.Shutdown(nil)
	}()
	select {
	case outcome := <-result:
		if outcome.recovered == nil || outcome.err != nil {
			t.Fatalf("Shutdown(nil) = error %v panic %v, want panic while active connection waits", outcome.err, outcome.recovered)
		}
		if message := fmt.Sprint(outcome.recovered); !strings.Contains(message, "invalid memory address or nil pointer dereference") {
			t.Fatalf("Shutdown(nil) panic = %q, want nil pointer dereference", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown(nil) did not return or panic")
	}
	cancelSSE()
	_ = sseResponse.Body.Close()
	waitForRuntimeConnections(t, running.metrics, 0, 0)
}

type lifecycleServer struct {
	server    *http.Server
	client    *http.Client
	url       string
	serveDone chan error
	serveExit chan struct{}
	metrics   *RuntimeMetrics
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

func startStreamingLifecycleServer(t *testing.T) *lifecycleServer {
	t.Helper()
	metrics := NewRuntimeMetrics()
	serverHandler := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	serverHandler.Use(observeRequests(metrics))
	registerStreams(serverHandler, metrics)
	serverHandler.MustMount(ghttp.RawOperation(http.MethodGet, "/__test/metrics", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, metrics.Snapshot())
	})))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server := &http.Server{Handler: serverHandler, ReadHeaderTimeout: time.Second}
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
		metrics:   metrics,
	}
	t.Cleanup(func() {
		_ = server.Close()
		select {
		case <-serveExit:
		case <-time.After(2 * time.Second):
			t.Error("streaming Serve goroutine did not exit during cleanup")
		}
	})
	return running
}

func waitForRuntimeConnections(t *testing.T, metrics *RuntimeMetrics, wantSSE, wantWS uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		snapshot := metrics.Snapshot()
		if snapshot.SSEConnections == wantSSE && snapshot.WSConnections == wantWS {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("runtime connections = (%d, %d), want (%d, %d)", snapshot.SSEConnections, snapshot.WSConnections, wantSSE, wantWS)
		default:
		}
	}
}

func waitForActiveRequests(t *testing.T, running *lifecycleServer, minimum uint64) {
	t.Helper()
	waitForLifecycleMetric(t, running, func(snapshot MetricsSnapshot) bool {
		return snapshot.ActiveRequests >= minimum
	})
}

func waitForLifecycleConnections(t *testing.T, running *lifecycleServer, wantSSE, wantWS uint64) {
	t.Helper()
	waitForLifecycleMetric(t, running, func(snapshot MetricsSnapshot) bool {
		return snapshot.SSEConnections == wantSSE && snapshot.WSConnections == wantWS
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

func dialLifecycleWebSocket(t *testing.T, rawURL string) *websocket.Conn {
	t.Helper()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(rawURL, "http"), nil)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		t.Fatalf("WebSocket Dial() error = %v", err)
	}
	return conn
}

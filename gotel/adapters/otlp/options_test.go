package otlp

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
)

func TestInvalidConfig(t *testing.T) {
	for _, opts := range [][]Option{
		{WithProtocol("invalid")}, {WithTimeout(0)}, {WithMetricInterval(0)}, {WithTraces(false), WithMetrics(false)},
		{WithEndpointURL("localhost:4318")}, {WithEndpointURL("ftp://localhost")}, {WithEndpointURL("https://")},
		{WithEndpointURL("http://user:password@localhost")}, {WithEndpointURL("http://localhost?secret=value")},
		{WithEndpointURL("http://localhost/#fragment")}, {WithEndpointURL("http://localhost:bad")},
		{WithProtocol(ProtocolGRPC), WithEndpointURL("http://localhost/path")},
		{WithEndpointURL("http://localhost"), WithTLSConfig(&tls.Config{})},
	} {
		if _, err := New(context.Background(), opts...); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("invalid configuration = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled construction = %v", err)
	}
}

func TestConfigCopiesMutableOptions(t *testing.T) {
	headers := map[string]string{"Authorization": "original"}
	tlsConfig := &tls.Config{ServerName: "original"}
	opts := []Option{nil, WithHeaders(headers), WithTLSConfig(tlsConfig)}
	headers["Authorization"], tlsConfig.ServerName = "mutated", "mutated"
	first, err := newConfig(opts)
	if err != nil {
		t.Fatal(err)
	}
	first.headers["Authorization"], first.tlsConfig.ServerName = "changed", "changed"
	second, err := newConfig(opts)
	if err != nil {
		t.Fatal(err)
	}
	if second.headers["Authorization"] != "original" || second.tlsConfig.ServerName != "original" {
		t.Fatal("options share mutable state")
	}
	if second.signalEndpoint("", "traces") != "" {
		t.Fatal("empty endpoint should defer to SDK environment configuration")
	}
}

func TestProviderFailureCleansUpExporters(t *testing.T) {
	for _, signals := range [][2]bool{{true, false}, {false, true}, {true, true}} {
		_, err := New(context.Background(), WithEndpointURL("http://127.0.0.1:1"), WithTraces(signals[0]), WithMetrics(signals[1]),
			WithProviderOptions(oteladapter.WithResource(nil)))
		if !errors.Is(err, oteladapter.ErrInvalidConfig) {
			t.Fatalf("provider initialization error = %v", err)
		}
	}
}

func TestHTTPEnvironmentConfiguration(t *testing.T) {
	capture := &capturedExports{}
	server := httptest.NewServer(capture)
	defer server.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer%20test")
	p, err := New(context.Background(), WithMetricInterval(time.Hour), providerSettings())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	recordSignals(t, p)
	if err := p.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkExports(t, capture, true, true)
}

func TestHTTPExportFailureAndTimeout(t *testing.T) {
	for _, slow := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "timeout"}[slow], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if slow {
					select {
					case <-r.Context().Done():
						return
					case <-time.After(time.Second):
					}
				}
				http.Error(w, "invalid telemetry", 400)
			}))
			defer server.Close()
			p, err := New(context.Background(), WithEndpointURL(server.URL), WithTimeout(30*time.Millisecond), WithMetricInterval(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = p.Shutdown(context.Background()) }()
			recordSignals(t, p)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := p.ForceFlush(ctx); err == nil {
				t.Fatal("export error was lost")
			}
		})
	}
}

package otlp

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/gotel"
	oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
	metricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type capturedExports struct {
	mu      sync.Mutex
	traces  []*tracepb.ExportTraceServiceRequest
	metrics []*metricpb.ExportMetricsServiceRequest
	paths   []string
	headers []string
	err     error
}

func (c *capturedExports) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paths = append(c.paths, r.URL.Path)
	c.headers = append(c.headers, r.Header.Get("Authorization"))
	if r.Header.Get("Content-Type") != "application/x-protobuf" {
		c.err = io.ErrUnexpectedEOF
		http.Error(w, "expected protobuf", 400)
		return
	}
	var response proto.Message
	switch {
	case strings.HasSuffix(r.URL.Path, "traces") || r.URL.Path == "/custom-trace":
		request := &tracepb.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(data, request); err != nil {
			c.err = err
			http.Error(w, err.Error(), 400)
			return
		}
		c.traces = append(c.traces, request)
		response = &tracepb.ExportTraceServiceResponse{}
	case strings.HasSuffix(r.URL.Path, "metrics") || r.URL.Path == "/custom-metric":
		request := &metricpb.ExportMetricsServiceRequest{}
		if err := proto.Unmarshal(data, request); err != nil {
			c.err = err
			http.Error(w, err.Error(), 400)
			return
		}
		c.metrics = append(c.metrics, request)
		response = &metricpb.ExportMetricsServiceResponse{}
	default:
		http.NotFound(w, r)
		return
	}
	encoded, err := proto.Marshal(response)
	if err != nil {
		c.err = err
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(encoded)
}

func recordSignals(t *testing.T, p *oteladapter.Provider) {
	t.Helper()
	ctx := context.Background()
	_, span := p.Start(ctx, "checkout", gotel.WithSpanKind(gotel.SpanKindServer), gotel.WithSpanAttributes(gotel.KV("route", "/orders")))
	span.AddEvent("paid", gotel.KV("amount", 10))
	span.End()
	counter, err := p.Counter("requests", gotel.WithAttributeKeys("method", "route"))
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(ctx, 2, gotel.KV("method", "GET"))
	active, err := p.UpDownCounter("active")
	if err != nil {
		t.Fatal(err)
	}
	active.Add(ctx, 1)
	active.Add(ctx, -1)
	histogram, err := p.Histogram("duration", gotel.WithUnit("s"), gotel.WithHistogramBoundaries(0.1, 1))
	if err != nil {
		t.Fatal(err)
	}
	histogram.Record(ctx, 0.5)
	gauge, err := p.Gauge("temperature")
	if err != nil {
		t.Fatal(err)
	}
	gauge.Record(ctx, 21)
}

func checkResource(t *testing.T, attrs []*commonpb.KeyValue) {
	t.Helper()
	values := map[string]string{}
	for _, attr := range attrs {
		values[attr.GetKey()] = attr.GetValue().GetStringValue()
	}
	for key, want := range map[string]string{"service.name": "orders", "service.version": "1.2.3", "deployment.environment.name": "test"} {
		if values[key] != want {
			t.Fatalf("resource %s = %q, want %q", key, values[key], want)
		}
	}
}

func checkExports(t *testing.T, c *capturedExports, traces, metrics bool) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		t.Fatal(c.err)
	}
	if (len(c.traces) > 0) != traces || (len(c.metrics) > 0) != metrics {
		t.Fatalf("trace/metric requests = %d/%d", len(c.traces), len(c.metrics))
	}
	for _, header := range c.headers {
		if header != "Bearer test" {
			t.Fatalf("authorization = %q", header)
		}
	}
	if traces {
		resourceSpans := c.traces[0].GetResourceSpans()
		if len(resourceSpans) != 1 {
			t.Fatalf("resource spans = %v", resourceSpans)
		}
		checkResource(t, resourceSpans[0].GetResource().GetAttributes())
		span := resourceSpans[0].GetScopeSpans()[0].GetSpans()[0]
		if span.GetName() != "checkout" || len(span.GetTraceId()) != 16 || len(span.GetSpanId()) != 8 || len(span.GetEvents()) != 1 {
			t.Fatalf("exported span = %v", span)
		}
	}
	if metrics {
		resourceMetrics := c.metrics[0].GetResourceMetrics()
		if len(resourceMetrics) != 1 {
			t.Fatalf("resource metrics = %v", resourceMetrics)
		}
		checkResource(t, resourceMetrics[0].GetResource().GetAttributes())
		names := map[string]bool{}
		for _, scope := range resourceMetrics[0].GetScopeMetrics() {
			for _, metric := range scope.GetMetrics() {
				names[metric.GetName()] = true
				switch metric.GetName() {
				case "requests":
					point := metric.GetSum().GetDataPoints()[0]
					if point.GetAsDouble() != 2 || !metric.GetSum().GetIsMonotonic() || len(point.GetAttributes()) != 2 {
						t.Fatalf("counter = %v", metric)
					}
				case "active":
					if metric.GetSum().GetDataPoints()[0].GetAsDouble() != 0 || metric.GetSum().GetIsMonotonic() {
						t.Fatalf("up-down counter = %v", metric)
					}
				case "duration":
					if metric.GetHistogram().GetDataPoints()[0].GetSum() != 0.5 || metric.GetUnit() != "s" {
						t.Fatalf("histogram = %v", metric)
					}
				case "temperature":
					if metric.GetGauge().GetDataPoints()[0].GetAsDouble() != 21 {
						t.Fatalf("gauge = %v", metric)
					}
				}
			}
		}
		if len(names) != 4 {
			t.Fatalf("metric names = %v", names)
		}
	}
}

func providerSettings() Option {
	return WithProviderOptions(oteladapter.WithServiceName("orders"), oteladapter.WithServiceVersion("1.2.3"), oteladapter.WithResourceAttributes(gotel.KV("deployment.environment.name", "test")))
}

func TestHTTPExport(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
			capture := &capturedExports{}
			server := httptest.NewUnstartedServer(capture)
			if secure {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			opts := []Option{WithEndpointURL(server.URL + "/collector"), WithHeaders(map[string]string{"Authorization": "Bearer test"}), WithMetricInterval(time.Hour), providerSettings()}
			if secure {
				opts = append(opts, WithTLSConfig(server.Client().Transport.(*http.Transport).TLSClientConfig))
			}
			p, err := New(context.Background(), opts...)
			if err != nil {
				t.Fatal(err)
			}
			// Close before the deferred collector close, including the final metrics export.
			defer func() {
				if err := p.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			recordSignals(t, p)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := p.ForceFlush(ctx); err != nil {
				t.Fatal(err)
			}
			checkExports(t, capture, true, true)
			capture.mu.Lock()
			defer capture.mu.Unlock()
			for _, path := range capture.paths {
				if path != "/collector/v1/traces" && path != "/collector/v1/metrics" {
					t.Fatalf("path = %q", path)
				}
			}
		})
	}
}

func TestHTTPSeparateEndpointsAndSignals(t *testing.T) {
	for _, tt := range []struct {
		name            string
		traces, metrics bool
	}{{"jaeger", true, false}, {"metrics", false, true}, {"both", true, true}} {
		t.Run(tt.name, func(t *testing.T) {
			capture := &capturedExports{}
			server := httptest.NewServer(capture)
			defer server.Close()
			p, err := New(context.Background(), WithTraces(tt.traces), WithMetrics(tt.metrics), WithEndpointURL("http://127.0.0.1:1"),
				WithTraceEndpointURL(server.URL+"/custom-trace"), WithMetricEndpointURL(server.URL+"/custom-metric"),
				WithMetricInterval(time.Hour), WithHeaders(map[string]string{"Authorization": "Bearer test"}), providerSettings())
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
			checkExports(t, capture, tt.traces, tt.metrics)
		})
	}
}

type traceReceiver struct {
	tracepb.UnimplementedTraceServiceServer
	capture *capturedExports
}

func (s *traceReceiver) Export(ctx context.Context, request *tracepb.ExportTraceServiceRequest) (*tracepb.ExportTraceServiceResponse, error) {
	s.capture.mu.Lock()
	defer s.capture.mu.Unlock()
	s.capture.traces = append(s.capture.traces, request)
	md, _ := metadata.FromIncomingContext(ctx)
	s.capture.headers = append(s.capture.headers, strings.Join(md.Get("authorization"), ""))
	return &tracepb.ExportTraceServiceResponse{}, nil
}

type metricReceiver struct {
	metricpb.UnimplementedMetricsServiceServer
	capture *capturedExports
}

func (s *metricReceiver) Export(ctx context.Context, request *metricpb.ExportMetricsServiceRequest) (*metricpb.ExportMetricsServiceResponse, error) {
	s.capture.mu.Lock()
	defer s.capture.mu.Unlock()
	s.capture.metrics = append(s.capture.metrics, request)
	md, _ := metadata.FromIncomingContext(ctx)
	s.capture.headers = append(s.capture.headers, strings.Join(md.Get("authorization"), ""))
	return &metricpb.ExportMetricsServiceResponse{}, nil
}

func TestGRPCExport(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "grpc", true: "grpcs"}[secure], func(t *testing.T) {
			capture := &capturedExports{}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var serverOpts []grpc.ServerOption
			var clientTLS *tls.Config
			scheme := "http://"
			if secure {
				fixture := httptest.NewTLSServer(http.NotFoundHandler())
				serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: fixture.TLS.Certificates})))
				clientTLS = fixture.Client().Transport.(*http.Transport).TLSClientConfig
				fixture.Close()
				scheme = "https://"
			}
			server := grpc.NewServer(serverOpts...)
			tracepb.RegisterTraceServiceServer(server, &traceReceiver{capture: capture})
			metricpb.RegisterMetricsServiceServer(server, &metricReceiver{capture: capture})
			defer server.Stop()
			go func() { _ = server.Serve(listener) }()
			p, err := New(context.Background(), WithProtocol(ProtocolGRPC), WithEndpointURL(scheme+listener.Addr().String()), WithTLSConfig(clientTLS),
				WithHeaders(map[string]string{"authorization": "Bearer test"}), WithMetricInterval(time.Hour), providerSettings())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := p.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			recordSignals(t, p)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := p.ForceFlush(ctx); err != nil {
				t.Fatal(err)
			}
			checkExports(t, capture, true, true)
		})
	}
}

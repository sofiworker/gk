package prometheus

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/sofiworker/gk/ghttp"
	ghttpotel "github.com/sofiworker/gk/ghttp/adapters/gotel"
)

func TestHTTPMetrics(t *testing.T) {
	meter, registry := newTestMeter(t)
	middleware, err := ghttpotel.Metrics(meter)
	if err != nil {
		t.Fatal(err)
	}
	server := ghttp.NewServer()
	server.Use(middleware)
	if err := server.Register(ghttp.Get("/users/:id", func(context.Context, ghttp.RequestOf[ghttp.NoDataType]) (string, error) {
		return "ok", nil
	})); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/users/42", "/missing"} {
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	metrics := gather(t, registry)
	durations := metrics["http_server_request_duration_seconds"].GetMetric()
	if len(durations) != 2 {
		t.Fatalf("duration series = %v", durations)
	}
	seen := map[string]bool{}
	for _, point := range durations {
		labels := map[string]string{}
		for _, label := range point.GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}
		seen[labels["http_route"]+":"+labels["http_response_status_code"]] = true
		if len(labels) != 3 || point.GetHistogram().GetSampleCount() != 1 {
			t.Fatalf("duration point = %v", point)
		}
	}
	if !seen["/users/:id:200"] || !seen[":404"] {
		t.Fatalf("routes/statuses = %v", seen)
	}
	for _, point := range metrics["http_server_active_requests"].GetMetric() {
		if point.GetGauge().GetValue() != 0 {
			t.Fatalf("active requests = %v", point)
		}
	}
}

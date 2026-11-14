// Package otelprometheus creates a managed OTel metric provider, an isolated
// Prometheus registry and a scrape handler. It does not start an HTTP server.
package otelprometheus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
	otelexport "go.opentelemetry.io/otel/exporters/prometheus"
)

var ErrInvalidConfig = errors.New("gotel/otelprometheus: invalid configuration")

type Provider struct {
	*oteladapter.Provider
	registry *prom.Registry
	handler  http.Handler
}

func New(ctx context.Context, opts ...Option) (*Provider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := config{registry: prom.NewRegistry()}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if c.registry == nil {
		return nil, fmt.Errorf("%w: nil registry", ErrInvalidConfig)
	}
	// OTel's unchecked collector cannot be unregistered. Keep it out of the
	// caller's registry so failed/repeated initialization leaves no collectors
	// behind there; only the handler combines both sets of metrics.
	exporterRegistry := prom.NewRegistry()
	exportOpts := append(c.exporterOptions, otelexport.WithRegisterer(exporterRegistry))
	exporter, err := otelexport.New(exportOpts...)
	if err != nil {
		return nil, fmt.Errorf("gotel/otelprometheus: create exporter: %w", err)
	}
	providerOpts := append(c.providerOptions, oteladapter.WithMetricReader(exporter))
	provider, err := oteladapter.NewProvider(ctx, providerOpts...)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return nil, errors.Join(err, exporter.Shutdown(cleanupCtx))
	}
	return &Provider{
		Provider: provider,
		registry: c.registry,
		handler:  promhttp.HandlerFor(prom.Gatherers{c.registry, exporterRegistry}, c.handlerOptions),
	}, nil
}

func (p *Provider) Handler() http.Handler { return p.handler }

// Registry returns the application's registry for additional collectors. OTel
// metrics live in a private registry; use Handler to gather both registries.
func (p *Provider) Registry() *prom.Registry { return p.registry }

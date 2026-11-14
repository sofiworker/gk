package otelprometheus

import (
	"slices"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
	otelexport "go.opentelemetry.io/otel/exporters/prometheus"
)

type config struct {
	registry        *prom.Registry
	exporterOptions []otelexport.Option
	handlerOptions  promhttp.HandlerOpts
	providerOptions []oteladapter.ProviderOption
}

type Option func(*config)

// WithRegistry includes a caller-owned registry in the scrape handler. OTel
// metrics use a separate private registry, preserving the supplied registry.
func WithRegistry(registry *prom.Registry) Option { return func(c *config) { c.registry = registry } }

func WithExporterOptions(opts ...otelexport.Option) Option {
	opts = slices.Clone(opts)
	return func(c *config) { c.exporterOptions = append(c.exporterOptions, opts...) }
}

func WithHandlerOptions(opts promhttp.HandlerOpts) Option {
	return func(c *config) { c.handlerOptions = opts }
}

func WithProviderOptions(opts ...oteladapter.ProviderOption) Option {
	opts = slices.Clone(opts)
	return func(c *config) { c.providerOptions = append(c.providerOptions, opts...) }
}

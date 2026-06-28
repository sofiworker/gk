package gws

import "net/http"

// ClientOption customizes runtime client behavior.
type ClientOption func(*clientOptions)

// ServiceOption customizes runtime handler behavior.
type ServiceOption func(*serviceOptions)

type clientOptions struct {
	SOAPVersion SOAPVersion
	HTTPClient  *http.Client
}

type serviceOptions struct {
	SOAPVersion SOAPVersion
}

func defaultClientOptions() clientOptions {
	return clientOptions{
		SOAPVersion: SOAP11,
		HTTPClient:  http.DefaultClient,
	}
}

func defaultServiceOptions() serviceOptions {
	return serviceOptions{
		SOAPVersion: SOAP11,
	}
}

func applyClientOptions(opts ...ClientOption) clientOptions {
	options := defaultClientOptions()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&options)
	}
	return options
}

func applyServiceOptions(opts ...ServiceOption) serviceOptions {
	options := defaultServiceOptions()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&options)
	}
	return options
}

// WithClientSOAPVersion sets the default SOAP version used by Client when a
// request operation does not declare one explicitly.
func WithClientSOAPVersion(version SOAPVersion) ClientOption {
	return func(options *clientOptions) {
		options.SOAPVersion = version
	}
}

// WithHTTPClient injects the underlying http.Client used by Client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(options *clientOptions) {
		options.HTTPClient = httpClient
	}
}

// WithServiceSOAPVersion sets the default SOAP version used by Handler when an
// operation does not declare one explicitly.
func WithServiceSOAPVersion(version SOAPVersion) ServiceOption {
	return func(options *serviceOptions) {
		options.SOAPVersion = version
	}
}

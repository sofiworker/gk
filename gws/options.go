package gws

type ClientOption func(*clientOptions)
type ServiceOption func(*serviceOptions)

type clientOptions struct {
	SOAPVersion SOAPVersion
}

type serviceOptions struct {
	SOAPVersion SOAPVersion
}

func defaultClientOptions() clientOptions {
	return clientOptions{
		SOAPVersion: SOAP11,
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

func WithClientSOAPVersion(version SOAPVersion) ClientOption {
	return func(options *clientOptions) {
		options.SOAPVersion = version
	}
}

func WithServiceSOAPVersion(version SOAPVersion) ServiceOption {
	return func(options *serviceOptions) {
		options.SOAPVersion = version
	}
}

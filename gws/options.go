package gws

type ClientOption func(*ClientOptions)
type ServiceOption func(*ServiceOptions)

type ClientOptions struct {
	SOAPVersion SOAPVersion
}

type ServiceOptions struct {
	SOAPVersion SOAPVersion
}

func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		SOAPVersion: SOAP11,
	}
}

func DefaultServiceOptions() ServiceOptions {
	return ServiceOptions{
		SOAPVersion: SOAP11,
	}
}

func NewClientOptions(opts ...ClientOption) ClientOptions {
	options := DefaultClientOptions()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&options)
	}
	return options
}

func NewServiceOptions(opts ...ServiceOption) ServiceOptions {
	options := DefaultServiceOptions()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&options)
	}
	return options
}

func WithClientSOAPVersion(version SOAPVersion) ClientOption {
	return func(options *ClientOptions) {
		options.SOAPVersion = version
	}
}

func WithServiceSOAPVersion(version SOAPVersion) ServiceOption {
	return func(options *ServiceOptions) {
		options.SOAPVersion = version
	}
}

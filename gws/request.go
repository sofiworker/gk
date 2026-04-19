package gws

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrEmptyEndpoint indicates that a request was built without a target
// endpoint.
var ErrEmptyEndpoint = errors.New("empty endpoint")

// ErrUnsupportedSOAPVersion indicates that the selected SOAP version is not
// supported by the runtime.
var ErrUnsupportedSOAPVersion = errors.New("unsupported SOAP version")

// ErrRequestWrapperMismatch indicates that the marshaled request body root
// element does not match the operation contract.
var ErrRequestWrapperMismatch = errors.New("request wrapper mismatch")

type operationOptions struct {
	ctx        context.Context
	endpoint   string
	operation  Operation
	header     http.Header
	soapHeader any
	body       any
}

// Request represents an outbound SOAP call that can be configured either at
// the logical body level or with a fully constructed envelope.
type Request struct {
	ctx        context.Context
	endpoint   string
	operation  Operation
	header     http.Header
	soapHeader any
	body       any
	envelope   *Envelope
}

// NewRequest creates a SOAP request bound to an endpoint and operation.
func NewRequest(ctx context.Context, endpoint string, operation Operation) *Request {
	return newRequest(operationOptions{
		ctx:       ctx,
		endpoint:  endpoint,
		operation: operation,
	})
}

func newRequest(opts operationOptions) *Request {
	ctx := opts.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	req := &Request{
		ctx:        ctx,
		endpoint:   opts.endpoint,
		operation:  opts.operation,
		header:     make(http.Header),
		soapHeader: opts.soapHeader,
		body:       opts.body,
	}

	for key, values := range opts.header {
		for _, value := range values {
			req.header.Add(key, value)
		}
	}

	return req
}

// Context returns the request context used when building the outbound
// http.Request.
func (r *Request) Context() context.Context {
	if r == nil {
		return nil
	}
	return r.ctx
}

// Endpoint returns the current target endpoint.
func (r *Request) Endpoint() string {
	if r == nil {
		return ""
	}
	return r.endpoint
}

// Operation returns the SOAP operation metadata associated with the request.
func (r *Request) Operation() Operation {
	if r == nil {
		return Operation{}
	}
	return r.operation
}

// Headers returns a copy of the HTTP headers configured on the request.
func (r *Request) Headers() http.Header {
	if r == nil {
		return nil
	}

	cloned := make(http.Header, len(r.header))
	for key, values := range r.header {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}

// SOAPHeader returns the currently configured SOAP header payload.
func (r *Request) SOAPHeader() any {
	if r == nil {
		return nil
	}
	return r.soapHeader
}

// Body returns the currently configured SOAP body payload.
func (r *Request) Body() any {
	if r == nil {
		return nil
	}
	return r.body
}

// SetHeader sets an HTTP header on the outbound request.
func (r *Request) SetHeader(key, value string) *Request {
	if r == nil {
		return nil
	}
	if r.header == nil {
		r.header = make(http.Header)
	}
	r.header.Set(key, value)
	return r
}

// SetSOAPHeader sets the SOAP header payload that will be marshaled into the
// request envelope.
func (r *Request) SetSOAPHeader(v any) *Request {
	if r == nil {
		return nil
	}
	r.soapHeader = v
	return r
}

// SetEndpoint overrides the target endpoint used when building the outbound
// HTTP request.
func (r *Request) SetEndpoint(endpoint string) *Request {
	if r == nil {
		return nil
	}
	r.endpoint = endpoint
	return r
}

// SetBody sets the logical SOAP body payload and clears any previously pinned
// low-level envelope.
func (r *Request) SetBody(v any) *Request {
	if r == nil {
		return nil
	}
	r.body = v
	r.envelope = nil
	return r
}

// SetEnvelope pins a fully constructed low-level SOAP envelope for this
// request.
func (r *Request) SetEnvelope(env Envelope) *Request {
	if r == nil {
		return nil
	}
	envCopy := env
	r.envelope = &envCopy
	return r
}

// Envelope returns the low-level SOAP envelope that will be marshaled for the
// request.
func (r *Request) Envelope() (Envelope, error) {
	if r == nil {
		return Envelope{}, ErrNilRequest
	}

	if r.envelope != nil {
		env := *r.envelope
		if env.Namespace == "" {
			soapEnv, err := resolveSOAPEnvelopeNamespace(r.operation.SOAPVersion)
			if err != nil {
				return Envelope{}, err
			}
			env.Namespace = soapEnv
		}
		return env, nil
	}

	if err := validateRequestWrapper(r.operation.RequestWrapper, r.body); err != nil {
		return Envelope{}, err
	}

	soapEnv, err := resolveSOAPEnvelopeNamespace(r.operation.SOAPVersion)
	if err != nil {
		return Envelope{}, err
	}

	env := Envelope{
		Namespace: soapEnv,
		Body: Body{
			Content: r.body,
		},
	}

	if r.soapHeader != nil {
		env.Header = &Header{Content: r.soapHeader}
	}

	return env, nil
}

// XMLBytes marshals the request into raw SOAP XML.
func (r *Request) XMLBytes() ([]byte, error) {
	if r == nil {
		return nil, ErrNilRequest
	}

	env, err := r.Envelope()
	if err != nil {
		return nil, err
	}

	data, err := MarshalEnvelope(env)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// BuildHTTPRequest builds the outbound POST request with SOAP headers and
// marshaled XML body.
func (r *Request) BuildHTTPRequest() (*http.Request, error) {
	if r == nil {
		return nil, ErrNilRequest
	}

	endpoint := strings.TrimSpace(r.endpoint)
	if endpoint == "" {
		return nil, ErrEmptyEndpoint
	}

	data, err := r.XMLBytes()
	if err != nil {
		return nil, fmt.Errorf("encode request xml: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(r.ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build http request: %w", err)
	}

	for key, values := range r.header {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	if httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "text/xml; charset=utf-8")
	}

	action := strings.TrimSpace(r.operation.Action)
	if action != "" {
		httpReq.Header.Set("SOAPAction", quoteSOAPAction(action))
	}

	return httpReq, nil
}

func quoteSOAPAction(action string) string {
	if len(action) >= 2 && action[0] == '"' && action[len(action)-1] == '"' {
		return action
	}
	return fmt.Sprintf("%q", action)
}

func resolveSOAPEnvelopeNamespace(version SOAPVersion) (string, error) {
	if version == "" {
		return SOAP11EnvelopeNamespace, nil
	}

	soapEnv, _ := SOAPNamespaces(version)
	if soapEnv == "" {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedSOAPVersion, version)
	}

	return soapEnv, nil
}

func validateRequestWrapper(expectWrapper xml.Name, body any) error {
	if isZeroXMLName(expectWrapper) {
		return nil
	}

	actualWrapper, err := requestBodyWrapperName(body)
	if err != nil {
		return err
	}

	if expectWrapper == actualWrapper {
		return nil
	}

	return fmt.Errorf(
		"%w: want=%s got=%s",
		ErrRequestWrapperMismatch,
		formatXMLName(expectWrapper),
		formatXMLName(actualWrapper),
	)
}

func requestBodyWrapperName(v any) (xml.Name, error) {
	if v == nil {
		return xml.Name{}, nil
	}

	data, err := xml.Marshal(v)
	if err != nil {
		return xml.Name{}, err
	}

	return firstElementName(data)
}

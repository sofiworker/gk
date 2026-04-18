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

var ErrEmptyEndpoint = errors.New("empty endpoint")

type operationOptions struct {
	ctx        context.Context
	endpoint   string
	operation  Operation
	header     http.Header
	soapHeader any
	body       any
}

type Request struct {
	ctx        context.Context
	endpoint   string
	operation  Operation
	header     http.Header
	soapHeader any
	body       any
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

func (r *Request) SetSOAPHeader(v any) *Request {
	if r == nil {
		return nil
	}
	r.soapHeader = v
	return r
}

func (r *Request) SetEndpoint(endpoint string) *Request {
	if r == nil {
		return nil
	}
	r.endpoint = endpoint
	return r
}

func (r *Request) SetBody(v any) *Request {
	if r == nil {
		return nil
	}
	r.body = v
	return r
}

func (r *Request) XMLBytes() ([]byte, error) {
	if r == nil {
		return nil, ErrNilRequest
	}

	soapEnv, _ := SOAPNamespaces(r.operation.SOAPVersion)
	if soapEnv == "" {
		soapEnv = SOAP11EnvelopeNamespace
	}

	env := requestEnvelope{
		SoapEnv: soapEnv,
		Body: requestEnvelopeBody{
			Content: r.body,
		},
	}

	if r.soapHeader != nil {
		env.Header = &requestEnvelopeHeader{Content: r.soapHeader}
	}

	data, err := xml.Marshal(env)
	if err != nil {
		return nil, err
	}
	return data, nil
}

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

type requestEnvelope struct {
	XMLName xml.Name               `xml:"soapenv:Envelope"`
	SoapEnv string                 `xml:"xmlns:soapenv,attr"`
	Header  *requestEnvelopeHeader `xml:"soapenv:Header,omitempty"`
	Body    requestEnvelopeBody    `xml:"soapenv:Body"`
}

type requestEnvelopeHeader struct {
	Content any `xml:",any,omitempty"`
}

type requestEnvelopeBody struct {
	Content any `xml:",any,omitempty"`
}

func quoteSOAPAction(action string) string {
	if len(action) >= 2 && action[0] == '"' && action[len(action)-1] == '"' {
		return action
	}
	return fmt.Sprintf("%q", action)
}

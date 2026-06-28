package gws

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var (
	// ErrNilRequest indicates that a nil Request was passed to Client.
	ErrNilRequest = errors.New("nil request")
	// ErrNilHTTPRequest indicates that a nil http.Request was passed to a low-level client API.
	ErrNilHTTPRequest = errors.New("nil http request")
	// ErrResponseWrapperMismatch indicates that the SOAP response body root
	// element does not match the expected operation wrapper.
	ErrResponseWrapperMismatch = errors.New("response wrapper mismatch")
)

// Client executes SOAP requests over HTTP.
type Client struct {
	httpClient *http.Client
	options    clientOptions
}

// NewClient creates a SOAP client runtime.
func NewClient(opts ...ClientOption) *Client {
	options := applyClientOptions(opts...)
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		httpClient: httpClient,
		options:    options,
	}
}

// Do sends a typed SOAP request and unmarshals the response body into out.
func (c *Client) Do(req *Request, out any) error {
	if req == nil {
		return ErrNilRequest
	}

	op := req.operation
	if op.SOAPVersion == "" {
		defaultVersion := c.options.SOAPVersion
		if defaultVersion == "" {
			defaultVersion = SOAP11
		}
		op.SOAPVersion = defaultVersion
	}

	sendReq := *req
	sendReq.operation = op

	httpReq, err := sendReq.BuildHTTPRequest()
	if err != nil {
		return err
	}

	return c.DoHTTP(httpReq, op, out)
}

// DoRaw sends a typed SOAP request and returns the raw SOAP response XML.
func (c *Client) DoRaw(req *Request) ([]byte, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	op := req.operation
	if op.SOAPVersion == "" {
		defaultVersion := c.options.SOAPVersion
		if defaultVersion == "" {
			defaultVersion = SOAP11
		}
		op.SOAPVersion = defaultVersion
	}

	sendReq := *req
	sendReq.operation = op

	httpReq, err := sendReq.BuildHTTPRequest()
	if err != nil {
		return nil, err
	}

	return c.DoHTTPRaw(httpReq, op)
}

// DoHTTP sends a prebuilt HTTP request and unmarshals the SOAP body into out.
func (c *Client) DoHTTP(req *http.Request, op Operation, out any) error {
	data, err := c.DoHTTPRaw(req, op)
	if err != nil {
		return err
	}

	if out == nil {
		if err := validateResponseWrapper(data, op.ResponseWrapper); err != nil {
			return err
		}
		return nil
	}

	if err := unmarshalSOAPBody(data, op.ResponseWrapper, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// DoHTTPRaw sends a prebuilt HTTP request and returns the raw SOAP response
// XML.
func (c *Client) DoHTTPRaw(req *http.Request, op Operation) ([]byte, error) {
	if req == nil {
		return nil, ErrNilHTTPRequest
	}

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	fault, err := ExtractFault(data)
	if err == nil {
		return nil, &FaultError{
			StatusCode: resp.StatusCode,
			Fault:      *fault,
		}
	}

	if !errors.Is(err, ErrFaultNotFound) {
		return nil, fmt.Errorf("extract fault: %w", err)
	}

	return data, nil
}

type responseEnvelope struct {
	Body responseBody `xml:"Body"`
}

type responseBody struct {
	InnerXML string `xml:",innerxml"`
}

func unmarshalSOAPBody(data []byte, expectWrapper xml.Name, out any) error {
	return UnmarshalBody(data, expectWrapper, out)
}

func validateResponseWrapper(data []byte, expectWrapper xml.Name) error {
	if isZeroXMLName(expectWrapper) {
		return nil
	}

	_, actualWrapper, err := DecodeBodyPayload(data)
	if err != nil {
		return err
	}

	return checkResponseWrapper(expectWrapper, actualWrapper)
}

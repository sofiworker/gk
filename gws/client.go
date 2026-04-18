package gws

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	ErrNilRequest              = errors.New("nil request")
	ErrNilHTTPRequest          = errors.New("nil http request")
	ErrResponseWrapperMismatch = errors.New("response wrapper mismatch")
)

type Client struct {
	httpClient *http.Client
	options    clientOptions
}

func NewClient(opts ...ClientOption) *Client {
	options := applyClientOptions(opts...)
	return &Client{
		httpClient: http.DefaultClient,
		options:    options,
	}
}

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

func (c *Client) DoHTTP(req *http.Request, op Operation, out any) error {
	if req == nil {
		return ErrNilHTTPRequest
	}

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	fault, err := extractFault(data)
	if err == nil {
		return &FaultError{
			StatusCode: resp.StatusCode,
			Fault:      *fault,
		}
	}

	if !errors.Is(err, ErrFaultNotFound) {
		return fmt.Errorf("extract fault: %w", err)
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

type responseEnvelope struct {
	Body responseBody `xml:"Body"`
}

type responseBody struct {
	InnerXML string `xml:",innerxml"`
}

func unmarshalSOAPBody(data []byte, expectWrapper xml.Name, out any) error {
	payload, actualWrapper, err := decodeSOAPBodyPayload(data)
	if err != nil {
		return err
	}

	if err := checkResponseWrapper(expectWrapper, actualWrapper); err != nil {
		return err
	}

	if len(payload) == 0 {
		return nil
	}

	return xml.Unmarshal(payload, out)
}

func validateResponseWrapper(data []byte, expectWrapper xml.Name) error {
	if isZeroXMLName(expectWrapper) {
		return nil
	}

	_, actualWrapper, err := decodeSOAPBodyPayload(data)
	if err != nil {
		return err
	}

	return checkResponseWrapper(expectWrapper, actualWrapper)
}

func decodeSOAPBodyPayload(data []byte) ([]byte, xml.Name, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, xml.Name{}, nil
	}

	var env responseEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, xml.Name{}, err
	}

	payload := strings.TrimSpace(env.Body.InnerXML)
	if payload == "" {
		return nil, xml.Name{}, nil
	}

	name, err := firstElementName([]byte(payload))
	if err != nil {
		return nil, xml.Name{}, err
	}

	return []byte(payload), name, nil
}

func firstElementName(data []byte) (xml.Name, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return xml.Name{}, nil
			}
			return xml.Name{}, err
		}

		if start, ok := token.(xml.StartElement); ok {
			return start.Name, nil
		}
	}
}

func checkResponseWrapper(expectWrapper, actualWrapper xml.Name) error {
	if isZeroXMLName(expectWrapper) {
		return nil
	}

	if expectWrapper == actualWrapper {
		return nil
	}

	return fmt.Errorf(
		"%w: want=%s got=%s",
		ErrResponseWrapperMismatch,
		formatXMLName(expectWrapper),
		formatXMLName(actualWrapper),
	)
}

func isZeroXMLName(name xml.Name) bool {
	return name.Local == "" && name.Space == ""
}

func formatXMLName(name xml.Name) string {
	if isZeroXMLName(name) {
		return "<empty>"
	}
	if name.Space == "" {
		return name.Local
	}
	return fmt.Sprintf("{%s}%s", name.Space, name.Local)
}

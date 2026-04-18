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
	ErrNilRequest     = errors.New("nil request")
	ErrNilHTTPRequest = errors.New("nil http request")
)

type Client struct {
	httpClient *http.Client
}

func NewClient(opts ...ClientOption) *Client {
	_ = applyClientOptions(opts...)
	return &Client{
		httpClient: http.DefaultClient,
	}
}

func (c *Client) Do(req *Request, out any) error {
	if req == nil {
		return ErrNilRequest
	}

	httpReq, err := req.BuildHTTPRequest()
	if err != nil {
		return err
	}

	return c.DoHTTP(httpReq, req.operation, out)
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
		return nil
	}

	if err := unmarshalSOAPBody(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	_ = op
	return nil
}

type responseEnvelope struct {
	Body responseBody `xml:"Body"`
}

type responseBody struct {
	InnerXML string `xml:",innerxml"`
}

func unmarshalSOAPBody(data []byte, out any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	var env responseEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return err
	}

	payload := strings.TrimSpace(env.Body.InnerXML)
	if payload == "" {
		return nil
	}

	return xml.Unmarshal([]byte(payload), out)
}

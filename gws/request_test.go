package gws

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"testing"
)

func TestNewRequest(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "trace")
	op := Operation{
		Name:           "Echo",
		Action:         "urn:Echo",
		RequestWrapper: xml.Name{Space: "urn:test", Local: "Echo"},
	}

	req := NewRequest(ctx, "http://example.com/ws", op)
	if req == nil {
		t.Fatal("NewRequest returned nil")
	}

	if got := req.ctx; got != ctx {
		t.Fatal("NewRequest did not keep context")
	}
	if got := req.endpoint; got != "http://example.com/ws" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
	if got := req.operation; got != op {
		t.Fatalf("unexpected operation: %+v", got)
	}
}

func TestRequestBuildHTTPRequest(t *testing.T) {
	req := newRequest(operationOptions{
		endpoint: "http://example.com/ws",
		operation: Operation{
			Name:           "Echo",
			Action:         "urn:Echo",
			RequestWrapper: xml.Name{Space: "urn:test", Local: "Echo"},
		},
	})
	req.SetBody(struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}{Value: "hello"})
	req.SetHeader("X-Trace-ID", "trace-1")

	httpReq, err := req.BuildHTTPRequest()
	if err != nil {
		t.Fatalf("BuildHTTPRequest failed: %v", err)
	}

	if got := httpReq.Method; got != "POST" {
		t.Fatalf("unexpected method: %q", got)
	}

	if got := httpReq.URL.String(); got != "http://example.com/ws" {
		t.Fatalf("unexpected endpoint: %q", got)
	}

	if got := httpReq.Header.Get("SOAPAction"); got != `"urn:Echo"` {
		t.Fatalf("unexpected soap action: %q", got)
	}

	if got := httpReq.Header.Get("X-Trace-ID"); got != "trace-1" {
		t.Fatalf("unexpected custom header: %q", got)
	}
}

func TestRequestWrapperMismatch(t *testing.T) {
	req := newRequest(operationOptions{
		endpoint: "http://example.com/ws",
		operation: Operation{
			Name:           "Echo",
			Action:         "urn:Echo",
			RequestWrapper: xml.Name{Space: "urn:test", Local: "Another"},
		},
	})
	req.SetBody(struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}{Value: "hello"})

	_, err := req.BuildHTTPRequest()
	if err == nil {
		t.Fatal("BuildHTTPRequest should fail when request wrapper mismatched")
	}

	if !errors.Is(err, ErrRequestWrapperMismatch) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRequestXMLBytes(t *testing.T) {
	req := newRequest(operationOptions{
		endpoint: "http://example.com/ws",
		operation: Operation{
			Name: "Echo",
		},
	})
	req.SetBody(struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}{Value: "hello"})

	data, err := req.XMLBytes()
	if err != nil {
		t.Fatalf("XMLBytes failed: %v", err)
	}

	if !bytes.Contains(data, []byte("<soapenv:Envelope")) {
		t.Fatalf("missing soap envelope: %s", data)
	}

	if !bytes.Contains(data, []byte("<soapenv:Body>")) {
		t.Fatalf("missing soap body: %s", data)
	}

	if !bytes.Contains(data, []byte("hello")) {
		t.Fatalf("missing payload: %s", data)
	}
}

func TestRequestSetEnvelope(t *testing.T) {
	req := NewRequest(context.Background(), "http://example.com/ws", Operation{
		Name:   "Echo",
		Action: "urn:Echo",
	})
	req.SetEnvelope(Envelope{
		Namespace: SOAP11EnvelopeNamespace,
		Header: &Header{
			Content: struct {
				XMLName xml.Name `xml:"urn:test TraceHeader"`
				Value   string   `xml:"value"`
			}{Value: "trace-1"},
		},
		Body: Body{
			Content: struct {
				XMLName xml.Name `xml:"urn:test Echo"`
				Value   string   `xml:"value"`
			}{Value: "hello"},
		},
	})

	env, err := req.Envelope()
	if err != nil {
		t.Fatalf("Envelope returned error: %v", err)
	}
	if env.Header == nil {
		t.Fatal("expected envelope header")
	}

	data, err := req.XMLBytes()
	if err != nil {
		t.Fatalf("XMLBytes failed: %v", err)
	}
	if !bytes.Contains(data, []byte("TraceHeader")) {
		t.Fatalf("missing custom envelope header: %s", data)
	}
}

func TestRequestAccessors(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "trace")
	op := Operation{
		Name:           "Echo",
		Action:         "urn:Echo",
		RequestWrapper: xml.Name{Space: "urn:test", Local: "Echo"},
	}
	body := &struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}{Value: "hello"}
	soapHeader := struct {
		XMLName xml.Name `xml:"urn:test TraceHeader"`
		Value   string   `xml:"value"`
	}{Value: "trace-1"}

	req := NewRequest(ctx, "http://example.com/ws", op)
	req.SetHeader("X-Trace-ID", "trace-1")
	req.SetSOAPHeader(soapHeader)
	req.SetBody(body)

	if got := req.Context(); got != ctx {
		t.Fatal("unexpected context accessor result")
	}
	if got := req.Endpoint(); got != "http://example.com/ws" {
		t.Fatalf("unexpected endpoint accessor result: %q", got)
	}
	if got := req.Operation(); got != op {
		t.Fatalf("unexpected operation accessor result: %+v", got)
	}

	headers := req.Headers()
	if got := headers.Get("X-Trace-ID"); got != "trace-1" {
		t.Fatalf("unexpected header accessor result: %q", got)
	}
	headers.Set("X-Trace-ID", "mutated")
	if got := req.Headers().Get("X-Trace-ID"); got != "trace-1" {
		t.Fatalf("headers accessor should return a copy, got: %q", got)
	}

	if got := req.SOAPHeader(); got != soapHeader {
		t.Fatalf("unexpected SOAP header accessor result: %#v", got)
	}
	if got := req.Body(); got != body {
		t.Fatalf("unexpected body accessor result: %#v", got)
	}
}

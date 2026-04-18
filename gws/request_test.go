package gws

import (
	"bytes"
	"encoding/xml"
	"errors"
	"testing"
)

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

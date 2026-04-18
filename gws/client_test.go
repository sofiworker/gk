package gws

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientDoOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("SOAPAction"); got != `"urn:Echo"` {
			t.Fatalf("unexpected soap action: %q", got)
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = w.Write([]byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
			<soapenv:Body>
				<EchoResponse xmlns="urn:test">
					<value>ok</value>
				</EchoResponse>
			</soapenv:Body>
		</soapenv:Envelope>`))
	}))
	defer srv.Close()

	req := newRequest(operationOptions{
		endpoint: srv.URL,
		operation: Operation{
			Name:            "Echo",
			Action:          "urn:Echo",
			ResponseWrapper: xml.Name{Space: "urn:test", Local: "EchoResponse"},
		},
	})
	req.SetBody(struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}{Value: "hello"})

	var out struct {
		XMLName xml.Name `xml:"urn:test EchoResponse"`
		Value   string   `xml:"value"`
	}

	client := NewClient()
	client.httpClient = srv.Client()
	if err := client.Do(req, &out); err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	if out.Value != "ok" {
		t.Fatalf("unexpected response value: %q", out.Value)
	}
}

func TestClientDoResponseWrapperMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = w.Write([]byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
			<soapenv:Body>
				<AnotherResponse xmlns="urn:test">
					<value>ok</value>
				</AnotherResponse>
			</soapenv:Body>
		</soapenv:Envelope>`))
	}))
	defer srv.Close()

	req := newRequest(operationOptions{
		endpoint: srv.URL,
		operation: Operation{
			Name:            "Echo",
			Action:          "urn:Echo",
			ResponseWrapper: xml.Name{Space: "urn:test", Local: "EchoResponse"},
		},
	})
	req.SetBody(struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}{Value: "hello"})

	client := NewClient()
	client.httpClient = srv.Client()
	err := client.Do(req, &struct{}{})
	if err == nil {
		t.Fatal("Do should fail when response wrapper mismatched")
	}

	if !errors.Is(err, ErrResponseWrapperMismatch) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientDoFault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
			<soapenv:Body>
				<soapenv:Fault>
					<faultcode>soap:Server</faultcode>
					<faultstring>boom</faultstring>
				</soapenv:Fault>
			</soapenv:Body>
		</soapenv:Envelope>`))
	}))
	defer srv.Close()

	req := newRequest(operationOptions{
		endpoint: srv.URL,
		operation: Operation{
			Name:   "Echo",
			Action: "urn:Echo",
		},
	})
	req.SetBody(struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}{Value: "hello"})

	client := NewClient()
	client.httpClient = srv.Client()
	err := client.Do(req, &struct{}{})
	if err == nil {
		t.Fatal("Do should fail when SOAP fault returned")
	}

	var faultErr *FaultError
	if !errors.As(err, &faultErr) {
		t.Fatalf("unexpected error type: %T", err)
	}

	if faultErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", faultErr.StatusCode)
	}

	if faultErr.Fault.Code != "soap:Server" {
		t.Fatalf("unexpected fault code: %q", faultErr.Fault.Code)
	}

	if faultErr.Fault.String != "boom" {
		t.Fatalf("unexpected fault string: %q", faultErr.Fault.String)
	}
}

func TestClientOptionDefaultSOAPVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("request should not be sent when SOAP version unsupported")
	}))
	defer srv.Close()

	req := newRequest(operationOptions{
		endpoint: srv.URL,
		operation: Operation{
			Name:   "Echo",
			Action: "urn:Echo",
		},
	})
	req.SetBody(struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}{Value: "hello"})

	client := NewClient(WithClientSOAPVersion("custom"))
	client.httpClient = srv.Client()
	err := client.Do(req, &struct{}{})
	if err == nil {
		t.Fatal("Do should fail when client default SOAP version unsupported")
	}

	if !errors.Is(err, ErrUnsupportedSOAPVersion) {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(err.Error(), "custom") {
		t.Fatalf("error should include unsupported version, got: %v", err)
	}
}

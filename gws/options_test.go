package gws

import (
	"net/http"
	"testing"
)

func TestDefaultClientOptions(t *testing.T) {
	opts := defaultClientOptions()
	if opts.SOAPVersion != SOAP11 {
		t.Fatalf("default SOAP version = %q, want %q", opts.SOAPVersion, SOAP11)
	}
}

func TestClientOptionsApply(t *testing.T) {
	httpClient := &http.Client{}
	opts := applyClientOptions(
		nil,
		WithClientSOAPVersion("custom"),
		WithHTTPClient(httpClient),
	)
	if opts.SOAPVersion != "custom" {
		t.Fatalf("client option not applied, got=%q", opts.SOAPVersion)
	}
	if opts.HTTPClient != httpClient {
		t.Fatal("http client option not applied")
	}
}

func TestDefaultServiceOptions(t *testing.T) {
	opts := defaultServiceOptions()
	if opts.SOAPVersion != SOAP11 {
		t.Fatalf("default SOAP version = %q, want %q", opts.SOAPVersion, SOAP11)
	}
}

func TestServiceOptionsApply(t *testing.T) {
	opts := applyServiceOptions(
		nil,
		WithServiceSOAPVersion("custom"),
	)
	if opts.SOAPVersion != "custom" {
		t.Fatalf("service option not applied, got=%q", opts.SOAPVersion)
	}
}

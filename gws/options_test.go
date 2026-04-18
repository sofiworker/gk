package gws

import "testing"

func TestDefaultClientOptions(t *testing.T) {
	opts := defaultClientOptions()
	if opts.SOAPVersion != SOAP11 {
		t.Fatalf("default SOAP version = %q, want %q", opts.SOAPVersion, SOAP11)
	}
}

func TestClientOptionsApply(t *testing.T) {
	opts := applyClientOptions(
		nil,
		WithClientSOAPVersion("custom"),
	)
	if opts.SOAPVersion != "custom" {
		t.Fatalf("client option not applied, got=%q", opts.SOAPVersion)
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

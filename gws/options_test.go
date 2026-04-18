package gws

import "testing"

func TestDefaultClientOptions(t *testing.T) {
	opts := DefaultClientOptions()
	if opts.SOAPVersion != SOAP11 {
		t.Fatalf("default SOAP version = %q, want %q", opts.SOAPVersion, SOAP11)
	}
}

func TestDefaultServiceOptions(t *testing.T) {
	opts := DefaultServiceOptions()
	if opts.SOAPVersion != SOAP11 {
		t.Fatalf("default SOAP version = %q, want %q", opts.SOAPVersion, SOAP11)
	}
}

package gws

import "testing"

func TestSOAPNamespaces(t *testing.T) {
	envNS, encNS := SOAPNamespaces(SOAP11)
	if envNS != SOAP11EnvelopeNamespace {
		t.Fatalf("envelope namespace = %q, want %q", envNS, SOAP11EnvelopeNamespace)
	}
	if encNS != SOAP11EncodingNamespace {
		t.Fatalf("encoding namespace = %q, want %q", encNS, SOAP11EncodingNamespace)
	}
}

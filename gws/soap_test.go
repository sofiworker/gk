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

func TestSOAPNamespacesUnknownVersion(t *testing.T) {
	envNS, encNS := SOAPNamespaces("invalid")
	if envNS != "" || encNS != "" {
		t.Fatalf("unknown SOAP version should return empty namespaces, got env=%q enc=%q", envNS, encNS)
	}
}

func TestSOAPNamespacesEmptyVersion(t *testing.T) {
	envNS, encNS := SOAPNamespaces("")
	if envNS != SOAP11EnvelopeNamespace {
		t.Fatalf("empty SOAP version envelope namespace = %q, want %q", envNS, SOAP11EnvelopeNamespace)
	}
	if encNS != SOAP11EncodingNamespace {
		t.Fatalf("empty SOAP version encoding namespace = %q, want %q", encNS, SOAP11EncodingNamespace)
	}
}

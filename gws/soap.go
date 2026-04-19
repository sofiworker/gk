package gws

// SOAPVersion identifies the SOAP envelope version used on the wire.
type SOAPVersion string

const (
	// SOAP11 identifies the SOAP 1.1 envelope version.
	SOAP11 SOAPVersion = "1.1"
)

const (
	// SOAP11EnvelopeNamespace is the SOAP 1.1 envelope namespace URI.
	SOAP11EnvelopeNamespace = "http://schemas.xmlsoap.org/soap/envelope/"
	// SOAP11EncodingNamespace is the SOAP 1.1 encoding namespace URI.
	SOAP11EncodingNamespace = "http://schemas.xmlsoap.org/soap/encoding/"
)

// SOAPNamespaces returns the envelope and encoding namespace for the given SOAP version.
func SOAPNamespaces(version SOAPVersion) (envelopeNamespace string, encodingNamespace string) {
	switch version {
	case SOAP11, "":
		return SOAP11EnvelopeNamespace, SOAP11EncodingNamespace
	default:
		return "", ""
	}
}

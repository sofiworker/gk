package gws

type SOAPVersion string

const (
	SOAP11 SOAPVersion = "1.1"
)

const (
	SOAP11EnvelopeNamespace = "http://schemas.xmlsoap.org/soap/envelope/"
	SOAP11EncodingNamespace = "http://schemas.xmlsoap.org/soap/encoding/"
)

// SOAPNamespaces returns the envelope and encoding namespace for the given SOAP version.
func SOAPNamespaces(version SOAPVersion) (envelopeNamespace string, encodingNamespace string) {
	switch version {
	case SOAP11, "":
		return SOAP11EnvelopeNamespace, SOAP11EncodingNamespace
	default:
		return SOAP11EnvelopeNamespace, SOAP11EncodingNamespace
	}
}

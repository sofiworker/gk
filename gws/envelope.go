package gws

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
)

// ErrEmptyEnvelopeData indicates that envelope decoding was attempted on an
// empty payload.
var ErrEmptyEnvelopeData = errors.New("empty SOAP envelope data")

// Envelope is the low-level SOAP envelope model exposed for direct library use.
type Envelope struct {
	XMLName   xml.Name `xml:"soapenv:Envelope"`
	Namespace string   `xml:"xmlns:soapenv,attr"`
	Header    *Header  `xml:"soapenv:Header,omitempty"`
	Body      Body     `xml:"soapenv:Body"`
}

// Header is the SOAP header container.
type Header struct {
	Content any `xml:",any,omitempty"`
}

// Body is the SOAP body container.
type Body struct {
	Content any            `xml:",any,omitempty"`
	Fault   *EnvelopeFault `xml:"soapenv:Fault,omitempty"`
}

// EnvelopeFault is the wire-level SOAP fault shape used inside an envelope.
type EnvelopeFault struct {
	Code   string       `xml:"faultcode"`
	String string       `xml:"faultstring"`
	Actor  string       `xml:"faultactor,omitempty"`
	Detail *FaultDetail `xml:"detail,omitempty"`
}

// FaultDetail preserves raw inner XML for SOAP fault details.
type FaultDetail struct {
	InnerXML string `xml:",innerxml"`
}

type envelopeForDecode struct {
	XMLName xml.Name      `xml:"Envelope"`
	Body    bodyForDecode `xml:"Body"`
}

type bodyForDecode struct {
	Fault *EnvelopeFault `xml:"Fault"`
}

// MarshalEnvelope marshals a low-level SOAP envelope.
func MarshalEnvelope(v Envelope) ([]byte, error) {
	if v.Namespace == "" {
		v.Namespace, _ = SOAPNamespaces(SOAP11)
	}

	data, err := xml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// UnmarshalEnvelope unmarshals a SOAP envelope and extracts the fault section when present.
func UnmarshalEnvelope(data []byte) (*Envelope, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrEmptyEnvelopeData
	}

	var decoded envelopeForDecode
	if err := xml.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}

	env := &Envelope{
		XMLName:   decoded.XMLName,
		Namespace: decoded.XMLName.Space,
		Body: Body{
			Fault: decoded.Body.Fault,
		},
	}

	if env.Namespace == "" {
		env.Namespace = SOAP11EnvelopeNamespace
	}

	return env, nil
}

// MarshalFaultEnvelope encodes a logical SOAP fault as a SOAP envelope.
func MarshalFaultEnvelope(fault Fault, version SOAPVersion) ([]byte, error) {
	if fault.Code == "" {
		fault.Code = "soap:Server"
	}
	if fault.String == "" {
		fault.String = "internal error"
	}

	namespace, err := resolveSOAPEnvelopeNamespace(version)
	if err != nil {
		return nil, err
	}

	envFault := &EnvelopeFault{
		Code:   fault.Code,
		String: fault.String,
		Actor:  fault.Actor,
	}
	if detail := marshalFaultDetailValue(fault.Detail); detail != "" {
		envFault.Detail = &FaultDetail{InnerXML: detail}
	}

	return MarshalEnvelope(Envelope{
		Namespace: namespace,
		Body: Body{
			Fault: envFault,
		},
	})
}

func marshalFaultDetailValue(v any) string {
	switch detail := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(detail)
	case []byte:
		return strings.TrimSpace(string(detail))
	default:
		data, err := xml.Marshal(detail)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
}

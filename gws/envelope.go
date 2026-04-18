package gws

import (
	"bytes"
	"encoding/xml"
	"errors"
)

var ErrEmptyEnvelopeData = errors.New("empty SOAP envelope data")

type envelope struct {
	XMLName xml.Name `xml:"soapenv:Envelope"`
	SoapEnv string   `xml:"xmlns:soapenv,attr"`
	Header  *header  `xml:"soapenv:Header,omitempty"`
	Body    body     `xml:"soapenv:Body"`
}

type header struct{}

type body struct {
	Content any            `xml:",any,omitempty"`
	Fault   *envelopeFault `xml:"soapenv:Fault,omitempty"`
}

type envelopeFault struct {
	Code   string       `xml:"faultcode"`
	String string       `xml:"faultstring"`
	Actor  string       `xml:"faultactor,omitempty"`
	Detail *faultDetail `xml:"detail,omitempty"`
}

type faultDetail struct {
	InnerXML string `xml:",innerxml"`
}

type envelopeForDecode struct {
	XMLName xml.Name      `xml:"Envelope"`
	SoapEnv string        `xml:"xmlns:soapenv,attr"`
	Body    bodyForDecode `xml:"Body"`
}

type bodyForDecode struct {
	Fault *envelopeFault `xml:"Fault"`
}

func marshalEnvelope(v envelope) ([]byte, error) {
	if v.SoapEnv == "" {
		v.SoapEnv, _ = SOAPNamespaces(SOAP11)
	}

	data, err := xml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func unmarshalEnvelope(data []byte) (*envelope, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrEmptyEnvelopeData
	}

	var decoded envelopeForDecode
	if err := xml.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}

	env := &envelope{
		XMLName: decoded.XMLName,
		SoapEnv: decoded.SoapEnv,
		Body: body{
			Fault: decoded.Body.Fault,
		},
	}

	if env.SoapEnv == "" {
		env.SoapEnv = env.XMLName.Space
	}

	return env, nil
}

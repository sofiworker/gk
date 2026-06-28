package gws

import (
	"bytes"
	"encoding/xml"
	"testing"
)

func TestMarshalEnvelopeWithBody(t *testing.T) {
	type payload struct {
		XMLName xml.Name `xml:"urn:test Echo"`
		Value   string   `xml:"value"`
	}

	data, err := MarshalEnvelope(Envelope{
		Body: Body{Content: payload{Value: "hello"}},
	})
	if err != nil {
		t.Fatalf("MarshalEnvelope failed: %v", err)
	}

	if !bytes.Contains(data, []byte("<soapenv:Envelope")) {
		t.Fatalf("unexpected xml: %s", data)
	}

	if !bytes.Contains(data, []byte(`xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"`)) {
		t.Fatalf("missing soapenv namespace: %s", data)
	}

	if !bytes.Contains(data, []byte("<soapenv:Body>")) {
		t.Fatalf("missing soapenv body: %s", data)
	}

	if !bytes.Contains(data, []byte("Echo")) {
		t.Fatalf("payload not found in xml: %s", data)
	}
}

func TestMarshalEnvelopeFaultWithoutDetail(t *testing.T) {
	data, err := MarshalEnvelope(Envelope{
		Body: Body{
			Fault: &EnvelopeFault{
				Code:   "soap:Server",
				String: "backend failed",
			},
		},
	})
	if err != nil {
		t.Fatalf("MarshalEnvelope failed: %v", err)
	}

	if !bytes.Contains(data, []byte("<soapenv:Fault>")) {
		t.Fatalf("fault should be namespaced: %s", data)
	}

	if bytes.Contains(data, []byte("<detail></detail>")) {
		t.Fatalf("empty detail should be omitted: %s", data)
	}
}

func TestUnmarshalFaultEnvelope(t *testing.T) {
	data := []byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
		<soapenv:Body>
			<soapenv:Fault>
				<faultcode>soap:Client</faultcode>
				<faultstring>invalid request</faultstring>
				<faultactor>urn:actor</faultactor>
				<detail><reason>missing field</reason></detail>
			</soapenv:Fault>
		</soapenv:Body>
	</soapenv:Envelope>`)

	env, err := UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope failed: %v", err)
	}

	if env == nil {
		t.Fatal("UnmarshalEnvelope returned nil envelope")
	}

	if env.Body.Fault == nil {
		t.Fatalf("fault is nil: %+v", env.Body)
	}

	if env.Body.Fault.Code != "soap:Client" {
		t.Fatalf("fault code = %q, want %q", env.Body.Fault.Code, "soap:Client")
	}
}

func TestUnmarshalEnvelopeEmptyData(t *testing.T) {
	_, err := UnmarshalEnvelope(nil)
	if err == nil {
		t.Fatal("UnmarshalEnvelope should fail for empty data")
	}

	if err != ErrEmptyEnvelopeData {
		t.Fatalf("error = %v, want %v", err, ErrEmptyEnvelopeData)
	}
}

func TestUnmarshalEnvelopeDefaultNamespaceFallback(t *testing.T) {
	data := []byte(`<Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/">
		<Body>
			<Fault>
				<faultcode>soap:Client</faultcode>
				<faultstring>invalid request</faultstring>
			</Fault>
		</Body>
	</Envelope>`)

	env, err := UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope failed: %v", err)
	}

	if env.Namespace != SOAP11EnvelopeNamespace {
		t.Fatalf("namespace = %q, want %q", env.Namespace, SOAP11EnvelopeNamespace)
	}
}

func TestMarshalFaultEnvelope(t *testing.T) {
	data, err := MarshalFaultEnvelope(Fault{
		Code:   "soap:Client",
		String: "bad request",
		Actor:  "urn:test",
		Detail: `<reason>missing field</reason>`,
	}, SOAP11)
	if err != nil {
		t.Fatalf("MarshalFaultEnvelope failed: %v", err)
	}

	text := string(data)
	if !bytes.Contains(data, []byte("<soapenv:Fault>")) {
		t.Fatalf("missing fault element: %s", text)
	}
	if !bytes.Contains(data, []byte("<faultstring>bad request</faultstring>")) {
		t.Fatalf("missing fault string: %s", text)
	}
	if !bytes.Contains(data, []byte("<detail><reason>missing field</reason></detail>")) {
		t.Fatalf("missing fault detail: %s", text)
	}
}

package gws

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestExtractFault(t *testing.T) {
	data := []byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
		<soapenv:Body>
			<soapenv:Fault>
				<faultcode>soap:Server</faultcode>
				<faultstring>backend failed</faultstring>
				<faultactor>urn:service</faultactor>
				<detail><trace>request-id-1</trace></detail>
			</soapenv:Fault>
		</soapenv:Body>
	</soapenv:Envelope>`)

	fault, err := ExtractFault(data)
	if err != nil {
		t.Fatalf("ExtractFault failed: %v", err)
	}

	if fault == nil {
		t.Fatal("ExtractFault returned nil fault")
	}

	if fault.Code != "soap:Server" {
		t.Fatalf("fault code = %q, want %q", fault.Code, "soap:Server")
	}

	if fault.String != "backend failed" {
		t.Fatalf("fault string = %q, want %q", fault.String, "backend failed")
	}

	if fault.Actor != "urn:service" {
		t.Fatalf("fault actor = %q, want %q", fault.Actor, "urn:service")
	}

	detail, ok := fault.Detail.(string)
	if !ok {
		t.Fatalf("fault detail type = %T, want string", fault.Detail)
	}

	if !strings.Contains(detail, "request-id-1") {
		t.Fatalf("fault detail = %q, want contains request-id-1", detail)
	}
}

func TestExtractFaultNotFound(t *testing.T) {
	data := []byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
		<soapenv:Body>
			<m:EchoResponse xmlns:m="urn:test"><m:value>ok</m:value></m:EchoResponse>
		</soapenv:Body>
	</soapenv:Envelope>`)

	_, err := ExtractFault(data)
	if err == nil {
		t.Fatal("ExtractFault should fail when fault is absent")
	}

	if err != ErrFaultNotFound {
		t.Fatalf("error = %v, want %v", err, ErrFaultNotFound)
	}
}

func TestDecodeBodyPayload(t *testing.T) {
	data := []byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
		<soapenv:Body>
			<m:EchoResponse xmlns:m="urn:test"><m:value>ok</m:value></m:EchoResponse>
		</soapenv:Body>
	</soapenv:Envelope>`)

	payload, wrapper, err := DecodeBodyPayload(data)
	if err != nil {
		t.Fatalf("DecodeBodyPayload failed: %v", err)
	}
	if wrapper.Space != "urn:test" || wrapper.Local != "EchoResponse" {
		t.Fatalf("unexpected wrapper: %+v", wrapper)
	}
	if !strings.Contains(string(payload), "ok") {
		t.Fatalf("unexpected payload: %s", payload)
	}
}

func TestUnmarshalBody(t *testing.T) {
	data := []byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
		<soapenv:Body>
			<m:EchoResponse xmlns:m="urn:test"><m:value>ok</m:value></m:EchoResponse>
		</soapenv:Body>
	</soapenv:Envelope>`)

	var out struct {
		XMLName xml.Name `xml:"urn:test EchoResponse"`
		Value   string   `xml:"value"`
	}

	if err := UnmarshalBody(data, xml.Name{Space: "urn:test", Local: "EchoResponse"}, &out); err != nil {
		t.Fatalf("UnmarshalBody failed: %v", err)
	}
	if out.Value != "ok" {
		t.Fatalf("unexpected value: %q", out.Value)
	}
}

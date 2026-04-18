package gws

import (
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

	fault, err := extractFault(data)
	if err != nil {
		t.Fatalf("extractFault failed: %v", err)
	}

	if fault == nil {
		t.Fatal("extractFault returned nil fault")
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

	_, err := extractFault(data)
	if err == nil {
		t.Fatal("extractFault should fail when fault is absent")
	}

	if err != ErrFaultNotFound {
		t.Fatalf("error = %v, want %v", err, ErrFaultNotFound)
	}
}

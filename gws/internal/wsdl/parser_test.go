package wsdl

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sofiworker/gk/gws/internal/model"
)

func TestParseEchoWSDL(t *testing.T) {
	wsdlPath := fixturePath(t, "echo.wsdl")

	m, err := ParseFile(wsdlPath)
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}

	if m.TargetNamespace != "urn:echo/wsdl" {
		t.Fatalf("unexpected target namespace: %q", m.TargetNamespace)
	}

	schema := findSchemaByNamespace(t, m.Schemas, "urn:echo/types")
	if len(schema.Elements) != 2 {
		t.Fatalf("unexpected element count: %d", len(schema.Elements))
	}
	echoRequest := findElementByName(t, schema.Elements, "EchoRequest")
	if echoRequest.Type.Space != "urn:echo/types" || echoRequest.Type.Local != "EchoRequestType" {
		t.Fatalf("unexpected EchoRequest type: %+v", echoRequest.Type)
	}
	echoReqType := findComplexTypeByName(t, schema.ComplexTypes, "EchoRequestType")
	if len(echoReqType.Fields) != 1 {
		t.Fatalf("unexpected EchoRequestType fields: %d", len(echoReqType.Fields))
	}
	if echoReqType.Fields[0].Name != "message" {
		t.Fatalf("unexpected field name: %q", echoReqType.Fields[0].Name)
	}
	if echoReqType.Fields[0].Type.Space != "http://www.w3.org/2001/XMLSchema" || echoReqType.Fields[0].Type.Local != "string" {
		t.Fatalf("unexpected field type: %+v", echoReqType.Fields[0].Type)
	}
	if echoReqType.Fields[0].MaxOccurs != 1 {
		t.Fatalf("unexpected maxOccurs: %d", echoReqType.Fields[0].MaxOccurs)
	}

	if len(m.Messages) != 2 {
		t.Fatalf("unexpected message count: %d", len(m.Messages))
	}
	requestMsg := findMessageByName(t, m.Messages, "EchoRequestMessage")
	if len(requestMsg.Parts) != 1 {
		t.Fatalf("unexpected EchoRequestMessage parts: %d", len(requestMsg.Parts))
	}
	if requestMsg.Parts[0].Element.Space != "urn:echo/types" || requestMsg.Parts[0].Element.Local != "EchoRequest" {
		t.Fatalf("unexpected request message part element: %+v", requestMsg.Parts[0].Element)
	}

	binding := findBindingByName(t, m.Bindings, "EchoBinding")
	if binding.Style != "document" {
		t.Fatalf("unexpected binding style: %q", binding.Style)
	}
	if binding.Type.Space != "urn:echo/wsdl" || binding.Type.Local != "EchoPortType" {
		t.Fatalf("unexpected binding type: %+v", binding.Type)
	}
	op := findBindingOperationByName(t, binding.Operations, "Echo")
	if op.Action != "urn:echo:Echo" {
		t.Fatalf("unexpected action: %q", op.Action)
	}
	if op.InputUse != "literal" || op.OutputUse != "literal" {
		t.Fatalf("unexpected use: input=%q output=%q", op.InputUse, op.OutputUse)
	}
	if op.InputNamespace != "urn:echo/types" {
		t.Fatalf("unexpected input namespace: %q", op.InputNamespace)
	}
	if op.InputMessage.Space != "urn:echo/wsdl" || op.InputMessage.Local != "EchoRequestMessage" {
		t.Fatalf("unexpected input message: %+v", op.InputMessage)
	}
	if op.OutputMessage.Space != "urn:echo/wsdl" || op.OutputMessage.Local != "EchoResponseMessage" {
		t.Fatalf("unexpected output message: %+v", op.OutputMessage)
	}

	service, ok := m.Service("EchoService")
	if !ok {
		t.Fatal("EchoService not found")
	}
	if len(service.Ports) != 1 {
		t.Fatalf("unexpected port count: %d", len(service.Ports))
	}
	if service.Ports[0].Binding.Space != "urn:echo/wsdl" || service.Ports[0].Binding.Local != "EchoBinding" {
		t.Fatalf("unexpected port binding: %+v", service.Ports[0].Binding)
	}
	if service.Ports[0].Address != "http://localhost:8080/echo" {
		t.Fatalf("unexpected address: %q", service.Ports[0].Address)
	}
}

func TestRejectUnsupportedChoice(t *testing.T) {
	data := loadFixture(t, "choice.wsdl")

	_, err := Parse(data, fixturePath(t, ""))
	if err == nil {
		t.Fatal("expected Parse to fail")
	}
	if !errors.Is(err, ErrUnsupportedXSDChoice) {
		t.Fatalf("expected ErrUnsupportedXSDChoice, got: %v", err)
	}
}

func fixturePath(t *testing.T, fileName string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve current file path")
	}

	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "wsdl", fileName)
}

func loadFixture(t *testing.T, fileName string) []byte {
	t.Helper()

	data, err := os.ReadFile(fixturePath(t, fileName))
	if err != nil {
		t.Fatalf("read fixture %q: %v", fileName, err)
	}
	return data
}

func findSchemaByNamespace(t *testing.T, schemas []model.Schema, targetNS string) model.Schema {
	t.Helper()

	for _, schema := range schemas {
		if schema.TargetNamespace == targetNS {
			return schema
		}
	}

	t.Fatalf("schema %q not found", targetNS)
	return model.Schema{}
}

func findElementByName(t *testing.T, elements []model.Element, name string) model.Element {
	t.Helper()

	for _, elem := range elements {
		if elem.Name == name {
			return elem
		}
	}

	t.Fatalf("element %q not found", name)
	return model.Element{}
}

func findComplexTypeByName(t *testing.T, types []model.ComplexType, name string) model.ComplexType {
	t.Helper()

	for _, ct := range types {
		if ct.Name == name {
			return ct
		}
	}

	t.Fatalf("complexType %q not found", name)
	return model.ComplexType{}
}

func findMessageByName(t *testing.T, messages []model.Message, name string) model.Message {
	t.Helper()

	for _, message := range messages {
		if message.Name == name {
			return message
		}
	}

	t.Fatalf("message %q not found", name)
	return model.Message{}
}

func findBindingByName(t *testing.T, bindings []model.Binding, name string) model.Binding {
	t.Helper()

	for _, binding := range bindings {
		if binding.Name == name {
			return binding
		}
	}

	t.Fatalf("binding %q not found", name)
	return model.Binding{}
}

func findBindingOperationByName(t *testing.T, ops []model.BindingOperation, name string) model.BindingOperation {
	t.Helper()

	for _, op := range ops {
		if op.Name == name {
			return op
		}
	}

	t.Fatalf("binding operation %q not found", name)
	return model.BindingOperation{}
}

package gws

import (
	"context"
	"encoding/xml"
	"errors"
	"testing"
)

func TestNewHandlerNilDesc(t *testing.T) {
	_, err := NewHandler(nil, nil)
	if err == nil {
		t.Fatal("NewHandler should fail when desc is nil")
	}
	if !errors.Is(err, ErrNilServiceDesc) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceDescFindOperationByWrapper(t *testing.T) {
	desc := &ServiceDesc{
		Operations: []OperationDesc{
			{
				Operation: Operation{
					Name:           "Echo",
					RequestWrapper: xml.Name{Space: "urn:test", Local: "Echo"},
				},
			},
		},
	}

	got, ok := desc.FindOperationByWrapper(xml.Name{Space: "urn:test", Local: "Echo"})
	if !ok {
		t.Fatal("expected operation to be found")
	}
	if got.Operation.Name != "Echo" {
		t.Fatalf("unexpected operation name: %q", got.Operation.Name)
	}

	_, ok = desc.FindOperationByWrapper(xml.Name{Space: "urn:test", Local: "Missing"})
	if ok {
		t.Fatal("unexpected operation found")
	}
}

func TestServiceDescAssetAccessors(t *testing.T) {
	desc := &ServiceDesc{
		WSDL: &WSDLAssetSet{
			Main: []byte("<definitions/>"),
			XSD: map[string][]byte{
				"types.xsd": []byte("<schema/>"),
			},
		},
	}

	wsdlData, ok := desc.WSDLAsset()
	if !ok || string(wsdlData) != "<definitions/>" {
		t.Fatalf("unexpected wsdl asset: %q ok=%v", wsdlData, ok)
	}

	xsdData, ok := desc.XSDAsset("types.xsd")
	if !ok || string(xsdData) != "<schema/>" {
		t.Fatalf("unexpected xsd asset: %q ok=%v", xsdData, ok)
	}
}

func TestOperationDescHelpers(t *testing.T) {
	op := OperationDesc{
		NewRequest: func() any {
			return &struct{ Value string }{}
		},
		NewResponse: func() any {
			return &struct{ Value string }{}
		},
		Invoke: func(ctx context.Context, impl any, req any) (any, error) {
			return req, nil
		},
	}

	req := op.NewRequestValue()
	if _, ok := req.(*struct{ Value string }); !ok {
		t.Fatalf("unexpected request value type: %T", req)
	}

	resp := op.NewResponseValue()
	if _, ok := resp.(*struct{ Value string }); !ok {
		t.Fatalf("unexpected response value type: %T", resp)
	}

	out, err := op.InvokeWith(context.Background(), nil, req)
	if err != nil {
		t.Fatalf("InvokeWith returned error: %v", err)
	}
	if out != req {
		t.Fatalf("unexpected invoke result: %#v", out)
	}
}

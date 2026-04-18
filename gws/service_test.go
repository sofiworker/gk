package gws

import (
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

	got, ok := desc.findOperationByWrapper(xml.Name{Space: "urn:test", Local: "Echo"})
	if !ok {
		t.Fatal("expected operation to be found")
	}
	if got.Operation.Name != "Echo" {
		t.Fatalf("unexpected operation name: %q", got.Operation.Name)
	}

	_, ok = desc.findOperationByWrapper(xml.Name{Space: "urn:test", Local: "Missing"})
	if ok {
		t.Fatal("unexpected operation found")
	}
}

package model

import "testing"

func TestModelOperationLookup(t *testing.T) {
	m := Model{
		Services: []Service{
			{
				Name: "CalculatorService",
				Ports: []Port{
					{
						Name:    "CalculatorPort",
						Binding: QName{Local: "CalculatorBinding"},
					},
				},
			},
		},
		Bindings: []Binding{
			{
				Name: "CalculatorBinding",
				Operations: []BindingOperation{
					{Name: "Add"},
					{Name: "Subtract"},
				},
			},
		},
	}

	svc, ok := m.Service("CalculatorService")
	if !ok {
		t.Fatal("expected service to be found")
	}
	if svc.Name != "CalculatorService" {
		t.Fatalf("unexpected service: %q", svc.Name)
	}

	binding, ok := m.Binding("CalculatorBinding")
	if !ok {
		t.Fatal("expected binding to be found")
	}
	if binding.Name != "CalculatorBinding" {
		t.Fatalf("unexpected binding: %q", binding.Name)
	}

	op, ok := binding.Operation("Add")
	if !ok {
		t.Fatal("expected operation to be found")
	}
	if op.Name != "Add" {
		t.Fatalf("unexpected operation: %q", op.Name)
	}

	_, ok = m.Service("UnknownService")
	if ok {
		t.Fatal("unexpected service found")
	}

	_, ok = m.Binding("UnknownBinding")
	if ok {
		t.Fatal("unexpected binding found")
	}

	_, ok = binding.Operation("Multiply")
	if ok {
		t.Fatal("unexpected operation found")
	}
}

func TestQNameFields(t *testing.T) {
	bindingQName := QName{Space: "urn:test:bindings", Local: "CalculatorBinding"}
	typeQName := QName{Space: "urn:test:types", Local: "CalculatorPortType"}
	elementQName := QName{Space: "urn:test:elements", Local: "AddRequest"}

	m := Model{
		Schemas: []Schema{
			{
				Elements: []Element{
					{Name: "AddRequest", Type: typeQName},
				},
				ComplexTypes: []ComplexType{
					{
						Name: "AddPayload",
						Fields: []Field{
							{Name: "A", Type: QName{Space: "http://www.w3.org/2001/XMLSchema", Local: "int"}},
						},
					},
				},
			},
		},
		Messages: []Message{
			{
				Name: "AddInput",
				Parts: []MessagePart{
					{
						Name:    "parameters",
						Element: elementQName,
					},
				},
			},
		},
		Services: []Service{
			{
				Name: "CalculatorService",
				Ports: []Port{
					{
						Name:    "CalculatorPort",
						Binding: bindingQName,
					},
				},
			},
		},
		Bindings: []Binding{
			{
				Name: "CalculatorBinding",
				Type: typeQName,
				Operations: []BindingOperation{
					{Name: "Add"},
				},
			},
		},
	}

	svc, ok := m.Service("CalculatorService")
	if !ok {
		t.Fatal("expected service to be found")
	}
	if svc.Ports[0].Binding != bindingQName {
		t.Fatalf("unexpected port binding qname: %+v", svc.Ports[0].Binding)
	}

	binding, ok := m.Binding("CalculatorBinding")
	if !ok {
		t.Fatal("expected binding to be found")
	}
	if binding.Type != typeQName {
		t.Fatalf("unexpected binding type qname: %+v", binding.Type)
	}

	if m.Schemas[0].Elements[0].Type != typeQName {
		t.Fatalf("unexpected element type qname: %+v", m.Schemas[0].Elements[0].Type)
	}

	part := m.Messages[0].Parts[0]
	if part.Element != elementQName {
		t.Fatalf("unexpected message part element qname: %+v", part.Element)
	}
}

func TestMaxOccursUnbounded(t *testing.T) {
	if MaxOccursUnbounded != -1 {
		t.Fatalf("MaxOccursUnbounded should be -1, got %d", MaxOccursUnbounded)
	}

	f := Field{
		Name:      "Items",
		Type:      QName{Space: "urn:test", Local: "Item"},
		MinOccurs: 0,
		MaxOccurs: MaxOccursUnbounded,
	}

	if f.MaxOccurs != MaxOccursUnbounded {
		t.Fatalf("unexpected max occurs: %d", f.MaxOccurs)
	}
}

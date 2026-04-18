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
						Binding: "CalculatorBinding",
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

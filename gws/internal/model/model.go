package model

func (m Model) Service(name string) (Service, bool) {
	for _, service := range m.Services {
		if service.Name == name {
			return service, true
		}
	}

	return Service{}, false
}

func (m Model) Binding(name string) (Binding, bool) {
	for _, binding := range m.Bindings {
		if binding.Name == name {
			return binding, true
		}
	}

	return Binding{}, false
}

func (b Binding) Operation(name string) (BindingOperation, bool) {
	for _, operation := range b.Operations {
		if operation.Name == name {
			return operation, true
		}
	}

	return BindingOperation{}, false
}

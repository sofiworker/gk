package gerr

import "strings"

type MultiError struct {
	ID     string
	Kind   Kind
	Params map[string]any
	Errors []error
}

func NewMulti(id string, kind Kind, errs []error, opts ...Option) *MultiError {
	filtered := filterErrors(errs)
	if len(filtered) == 0 {
		return nil
	}
	identity := New(id, kind, opts...)
	return &MultiError{ID: identity.ID, Kind: identity.Kind, Params: identity.Params, Errors: filtered}
}

func Join(errs ...error) error {
	filtered := filterErrors(errs)
	if len(filtered) == 0 {
		return nil
	}
	return &MultiError{Errors: filtered}
}

func filterErrors(errs []error) []error {
	filtered := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			filtered = append(filtered, err)
		}
	}
	return filtered
}

func (e *MultiError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := make([]string, 0, len(e.Errors))
	for _, err := range e.Errors {
		parts = append(parts, err.Error())
	}
	if e.ID == "" {
		return strings.Join(parts, "\n")
	}
	return e.ID + ": " + strings.Join(parts, "; ")
}

func (e *MultiError) ErrorDescriptor() Descriptor {
	if e == nil {
		return Descriptor{}
	}
	return Descriptor{ID: e.ID, Kind: e.Kind, Params: cloneStringMap(e.Params)}
}

func (e *MultiError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return append([]error(nil), e.Errors...)
}

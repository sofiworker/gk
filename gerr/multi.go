package gerr

import "strings"

type MultiError struct {
	Message string
	Errors  []error
}

func NewMulti(message string, errs ...error) error {
	filtered := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			filtered = append(filtered, err)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return &MultiError{
		Message: message,
		Errors:  filtered,
	}
}

func Join(errs ...error) error {
	return NewMulti("", errs...)
}

func (e *MultiError) Error() string {
	if e == nil {
		return "<nil>"
	}

	parts := make([]string, 0, len(e.Errors))
	for _, err := range e.Errors {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}

	if e.Message == "" {
		return strings.Join(parts, "\n")
	}
	if len(parts) == 0 {
		return e.Message
	}
	return e.Message + ": " + strings.Join(parts, "; ")
}

func (e *MultiError) Unwrap() []error {
	if e == nil || len(e.Errors) == 0 {
		return nil
	}
	out := make([]error, len(e.Errors))
	copy(out, e.Errors)
	return out
}

package ghttp

import (
	"context"
)

// Validator validates request input after binding.
type Validator interface {
	Validate(ctx context.Context, input interface{}) error
}

// ValidationError describes a field that failed validation.
type ValidationError struct {
	Field   string
	Tag     string
	Value   interface{}
	Message string
}

func (e *ValidationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "validation failed on " + e.Field + " for " + e.Tag
}

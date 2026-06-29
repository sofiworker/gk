package ghttp

import (
	"context"

	playgroundValidator "github.com/go-playground/validator/v10"
)

// Validator validates request input after binding.
type Validator interface {
	Validate(ctx context.Context, input interface{}) error
}

type defaultValidator struct {
	validate *playgroundValidator.Validate
}

func newDefaultValidator() Validator {
	return &defaultValidator{validate: playgroundValidator.New()}
}

func (v *defaultValidator) Validate(ctx context.Context, input interface{}) error {
	return v.validate.StructCtx(ctx, input)
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

package ghttp

import (
	"context"
	"errors"

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

// FieldValidationError describes a field that failed validation.
type FieldValidationError struct {
	Field   string
	Tag     string
	Value   interface{}
	Message string
}

func (e *FieldValidationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "validation failed on " + e.Field + " for " + e.Tag
}

// ValidateFunc validates a parsed route input.
type ValidateFunc[Req any] func(context.Context, Req) error

// SimpleValidateFunc validates a parsed route input without needing context.
type SimpleValidateFunc[Req any] func(Req) error

type routeValidateFunc[Req any] func(context.Context, Req) error

type validateOptions struct {
	err error
}

// ValidateOption configures route-level validation behavior.
type ValidateOption func(*validateOptions)

// ValidationError maps a route-level validation failure to err.
func ValidationError(err error) ValidateOption {
	return func(opts *validateOptions) {
		opts.err = err
	}
}

func mappedValidationError(replacement, cause error) error {
	if replacement == nil {
		return cause
	}
	if cause == nil {
		return replacement
	}
	if he := AsError(replacement); he != nil {
		clone := *he
		if clone.Err == nil {
			clone.Err = cause
		} else {
			clone.Err = errors.Join(clone.Err, cause)
		}
		return &clone
	}
	return errors.Join(replacement, cause)
}

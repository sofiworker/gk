package ghttp

import (
	"context"
	"testing"

	playgroundValidator "github.com/go-playground/validator/v10"
)

func TestValidationErrorAdapterMapsTagsWithoutValues(t *testing.T) {
	type item struct {
		Email string `json:"email" validate:"required,email"`
		Role  string `json:"role" validate:"oneof=admin member"`
	}
	input := struct {
		Items []item `json:"items" validate:"dive"`
	}{Items: []item{{Email: "secret", Role: "root"}, {}}}
	validate := playgroundValidator.New()
	err := validate.Struct(input)
	result, ok := defaultValidationErrorAdapter{}.AdaptValidationError(context.Background(), validationStageError{err: err, input: input})
	if !ok || len(result.Details) != 4 {
		t.Fatalf("AdaptValidationError() = %#v, %v", result, ok)
	}
	if result.Details[0].Location != "body.items[0].email" || result.Details[0].MessageID != "validation.email" {
		t.Fatalf("first detail = %#v", result.Details[0])
	}
	for _, detail := range result.Details {
		if _, leaked := detail.Args["value"]; leaked {
			t.Fatalf("detail leaked value: %#v", detail)
		}
	}
}

func TestValidationErrorAdapterIgnoresUnmarkedValidatorError(t *testing.T) {
	validate := playgroundValidator.New()
	err := validate.Var("", "required")
	if _, ok := (defaultValidationErrorAdapter{}).AdaptValidationError(context.Background(), err); ok {
		t.Fatal("unmarked validator error should not be adapted")
	}
}

func TestValidationMessageIDNormalizesUnknownTag(t *testing.T) {
	if got := validationMessageID("custom-Rule"); got != "validation.custom_rule" {
		t.Fatalf("validationMessageID() = %q", got)
	}
	if got := validationMessageID("!!!"); got != "validation.unknown" {
		t.Fatalf("validationMessageID() = %q", got)
	}
}

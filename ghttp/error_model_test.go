package ghttp

import (
	"context"
	"testing"

	"github.com/sofiworker/gk/gerr"
)

func TestErrorDocumentCloneDoesNotSharePublicMaps(t *testing.T) {
	document := ErrorDocument{Status: 422, MessageID: "request.validation_failed", Args: map[string]any{"field": "email"}, Details: []ErrorDetail{{Location: "body.email", MessageID: "validation.email", Args: map[string]any{"value": "hidden"}}}}
	clone := cloneErrorDocument(document)
	clone.Args["field"] = "name"
	clone.Details[0].Args["value"] = "changed"
	if document.Args["field"] != "email" || document.Details[0].Args["value"] != "hidden" {
		t.Fatal("clone shares mutable maps")
	}
}

func TestErrorExtensionInterfacesCompile(t *testing.T) {
	var _ ErrorNormalizer = ErrorNormalizerFunc(func(context.Context, error) NormalizedError { return NormalizedError{} })
	var _ KindStatusMapper = KindStatusMapperFunc(func(gerr.Kind) int { return 500 })
	var _ ErrorObserver = ErrorObserverFunc(func(context.Context, ErrorObservation) {})
	var _ ErrorDetailAdapter = ErrorDetailAdapterFunc(func(context.Context, error) ([]ErrorDetail, bool) { return nil, false })
	var _ ValidationErrorAdapter = ValidationErrorAdapterFunc(func(context.Context, error) (ValidationResult, bool) { return ValidationResult{}, false })
}

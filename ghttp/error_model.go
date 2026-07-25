package ghttp

import (
	"context"

	"github.com/sofiworker/gk/gerr"
)

type ErrorDocument struct {
	Status    int            `json:"status"`
	MessageID string         `json:"message_id"`
	Args      map[string]any `json:"args"`
	Message   string         `json:"message,omitempty"`
	Details   []ErrorDetail  `json:"errors,omitempty"`
}

type ErrorDetail struct {
	Location  string         `json:"location"`
	MessageID string         `json:"message_id"`
	Args      map[string]any `json:"args"`
	Message   string         `json:"message,omitempty"`
}

type NormalizedError struct {
	Document         ErrorDocument
	Cause            error
	Kind             gerr.Kind
	Meta             map[string]any
	Op               string
	SuppressResponse bool
}

type ErrorNormalizer interface {
	NormalizeError(context.Context, error) NormalizedError
}

type ErrorNormalizerFunc func(context.Context, error) NormalizedError

func (f ErrorNormalizerFunc) NormalizeError(ctx context.Context, err error) NormalizedError {
	return f(ctx, err)
}

type KindStatusMapper interface {
	HTTPStatus(gerr.Kind) int
}

type KindStatusMapperFunc func(gerr.Kind) int

func (f KindStatusMapperFunc) HTTPStatus(kind gerr.Kind) int { return f(kind) }

type HTTPStatusCarrier interface {
	HTTPStatus() int
}

type ErrorDetailAdapter interface {
	AdaptErrorDetails(context.Context, error) ([]ErrorDetail, bool)
}

type ErrorDetailAdapterFunc func(context.Context, error) ([]ErrorDetail, bool)

func (f ErrorDetailAdapterFunc) AdaptErrorDetails(ctx context.Context, err error) ([]ErrorDetail, bool) {
	return f(ctx, err)
}

type ValidationResult struct {
	Details []ErrorDetail
}

type ValidationErrorAdapter interface {
	AdaptValidationError(context.Context, error) (ValidationResult, bool)
}

type ValidationErrorAdapterFunc func(context.Context, error) (ValidationResult, bool)

func (f ValidationErrorAdapterFunc) AdaptValidationError(ctx context.Context, err error) (ValidationResult, bool) {
	return f(ctx, err)
}

type ErrorObservation struct {
	Status            int
	MessageID         string
	Kind              gerr.Kind
	Op                string
	Cause             error
	Meta              map[string]any
	RequestedLanguage string
	Language          string
	LocalizationError error
	RendererError     error
	Committed         bool
}

type ErrorObserver interface {
	ObserveError(context.Context, ErrorObservation)
}

type ErrorObserverFunc func(context.Context, ErrorObservation)

func (f ErrorObserverFunc) ObserveError(ctx context.Context, observation ErrorObservation) {
	f(ctx, observation)
}

func cloneErrorDocument(document ErrorDocument) ErrorDocument {
	document.Args = clonePublicMap(document.Args)
	if len(document.Details) > 0 {
		document.Details = append([]ErrorDetail(nil), document.Details...)
		for i := range document.Details {
			document.Details[i].Args = clonePublicMap(document.Details[i].Args)
		}
	}
	return document
}

func clonePublicMap(values map[string]any) map[string]any {
	cloned, _ := sanitizePublicParams(values)
	return cloned
}

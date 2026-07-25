package ghttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/sofiworker/gk/gerr"
)

func TestDefaultErrorNormalizerMapsKinds(t *testing.T) {
	tests := map[gerr.Kind]int{
		gerr.KindInvalid: 400, gerr.KindNotFound: 404, gerr.KindConflict: 409,
		gerr.KindUnauthenticated: 401, gerr.KindPermission: 403, gerr.KindRateLimited: 429,
		gerr.KindUnavailable: 503, gerr.KindTimeout: 504, gerr.KindInternal: 500,
	}
	normalizer := newDefaultErrorNormalizer(nil, nil)
	for kind, want := range tests {
		t.Run(string(kind), func(t *testing.T) {
			got := normalizer.NormalizeError(context.Background(), gerr.New("user.failure", kind))
			if got.Document.Status != want || got.Document.MessageID != "user.failure" {
				t.Fatalf("NormalizeError() = %#v", got)
			}
		})
	}
}

func TestDefaultErrorNormalizerSafelyDegradesUnknownError(t *testing.T) {
	got := newDefaultErrorNormalizer(nil, nil).NormalizeError(context.Background(), errors.New("database password leaked"))
	if got.Document.Status != 500 || got.Document.MessageID != "internal.server_error" || len(got.Document.Args) != 0 {
		t.Fatalf("NormalizeError() = %#v", got)
	}
	if got.Cause == nil {
		t.Fatal("cause should remain available internally")
	}
}

func TestDefaultErrorNormalizerSuppressesCanceledResponse(t *testing.T) {
	got := newDefaultErrorNormalizer(nil, nil).NormalizeError(context.Background(), fmt.Errorf("upstream: %w", context.Canceled))
	if !got.SuppressResponse || got.Kind != gerr.KindCanceled {
		t.Fatalf("NormalizeError() = %#v", got)
	}
}

func TestDefaultErrorNormalizerMapsDeadline(t *testing.T) {
	got := newDefaultErrorNormalizer(nil, nil).NormalizeError(context.Background(), context.DeadlineExceeded)
	if got.Document.Status != http.StatusGatewayTimeout || got.Document.MessageID != "request.timeout" {
		t.Fatalf("NormalizeError() = %#v", got)
	}
}

func TestWithStatusUsesOutermostValidStatus(t *testing.T) {
	err := WithStatus(WithStatus(gerr.New("user.gone", gerr.KindNotFound), http.StatusGone), http.StatusPreconditionFailed)
	got := newDefaultErrorNormalizer(nil, nil).NormalizeError(context.Background(), err)
	if got.Document.Status != http.StatusPreconditionFailed || got.Document.MessageID != "user.gone" {
		t.Fatalf("NormalizeError() = %#v", got)
	}
}

func TestDefaultErrorNormalizerRejectsReservedBusinessID(t *testing.T) {
	got := newDefaultErrorNormalizer(nil, nil).NormalizeError(context.Background(), gerr.New("internal.custom", gerr.KindInvalid))
	if got.Document.Status != 500 || got.Document.MessageID != "internal.server_error" {
		t.Fatalf("NormalizeError() = %#v", got)
	}
}

func TestFinalizeNormalizedErrorSanitizesCustomOutput(t *testing.T) {
	unsafe := NormalizedError{Document: ErrorDocument{Status: 200, MessageID: "INVALID", Args: map[string]any{"error": errors.New("secret")}}}
	got := finalizeNormalizedError(unsafe)
	if got.Document.Status != 500 || got.Document.MessageID != "internal.server_error" || len(got.Document.Args) != 0 {
		t.Fatalf("finalizeNormalizedError() = %#v", got)
	}
}

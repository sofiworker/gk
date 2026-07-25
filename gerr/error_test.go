package gerr

import (
	"errors"
	"testing"
)

func TestNewCreatesPublicStructuredError(t *testing.T) {
	cause := errors.New("store unavailable")
	params := map[string]any{"user_id": 42}
	meta := map[string]any{"query": "select secret"}

	err := New(
		"user.not_found",
		KindNotFound,
		WithParams(params),
		WithMetadata(meta),
		WithOp("user.lookup"),
		WithMessage("lookup user"),
		WithCause(cause),
	)
	params["user_id"] = 7
	meta["query"] = "changed"

	if err.ID != "user.not_found" || err.Kind != KindNotFound {
		t.Fatalf("identity = (%q, %q)", err.ID, err.Kind)
	}
	if err.Params["user_id"] != 42 || err.Meta["query"] != "select secret" {
		t.Fatalf("maps were not copied: params=%v meta=%v", err.Params, err.Meta)
	}
	if !errors.Is(err, cause) || !errors.Is(err, &Error{ID: "user.not_found"}) {
		t.Fatal("structured error should match cause and ID")
	}
}

func TestWrapAddsInternalContextWithoutPublicIdentity(t *testing.T) {
	cause := errors.New("store unavailable")
	err := Wrap(cause, WithOp("user.lookup"), WithMessage("lookup user"), WithMeta("user_id", 42))

	if !errors.Is(err, cause) {
		t.Fatal("wrapped error should match cause")
	}
	descriptor, ok := Describe(err)
	if ok {
		t.Fatalf("Describe(internal wrapper) = %#v, true; want false", descriptor)
	}
}

func TestWrapCanReclassifyError(t *testing.T) {
	err := Wrap(errors.New("missing"), WithID("user.not_found"), WithKind(KindNotFound), WithParam("user_id", 42))
	descriptor, ok := Describe(err)
	if !ok || descriptor.ID != "user.not_found" || descriptor.Params["user_id"] != 42 {
		t.Fatalf("Describe() = %#v, %v", descriptor, ok)
	}
}

func TestWrapNilReturnsNil(t *testing.T) {
	if err := Wrap(nil, WithMessage("ignored")); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
}

func TestKindConstants(t *testing.T) {
	for _, kind := range []Kind{KindInvalid, KindNotFound, KindConflict, KindUnauthenticated, KindPermission, KindRateLimited, KindUnavailable, KindTimeout, KindCanceled, KindInternal} {
		if kind == KindUnknown {
			t.Fatal("public kind must not be unknown")
		}
	}
}

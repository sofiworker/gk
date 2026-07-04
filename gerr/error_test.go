package gerr

import (
	"errors"
	"testing"
)

func TestErrorWrapsCauseAndMatchesCodeKind(t *testing.T) {
	cause := errors.New("store unavailable")

	err := Wrap(
		cause,
		"query user",
		WithCode("db.query"),
		WithKind(KindUnavailable),
		WithOp("user.lookup"),
		WithMeta("user_id", 42),
	)

	if !errors.Is(err, cause) {
		t.Fatal("wrapped error should match cause")
	}
	if !errors.Is(err, &Error{Code: "db.query"}) {
		t.Fatal("wrapped error should match target error code")
	}
	if !IsCode(err, "db.query") {
		t.Fatal("IsCode should find wrapped code")
	}
	if !IsKind(err, KindUnavailable) {
		t.Fatal("IsKind should find wrapped kind")
	}

	var got *Error
	if !errors.As(err, &got) {
		t.Fatal("wrapped error should expose *Error with errors.As")
	}
	if got.Op != "user.lookup" {
		t.Fatalf("Op = %q, want user.lookup", got.Op)
	}
	if got.Meta["user_id"] != 42 {
		t.Fatalf("Meta[user_id] = %v, want 42", got.Meta["user_id"])
	}
}

func TestNewErrorWithoutCauseMatchesCodeAndKind(t *testing.T) {
	err := New("invalid request", WithCode("request.invalid"), WithKind(KindInvalid))

	if !IsCode(err, "request.invalid") {
		t.Fatal("IsCode should match direct error code")
	}
	if !IsKind(err, KindInvalid) {
		t.Fatal("IsKind should match direct error kind")
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("new error without cause should not unwrap")
	}
}

func TestWrapNilReturnsNil(t *testing.T) {
	if err := Wrap(nil, "ignored"); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
}

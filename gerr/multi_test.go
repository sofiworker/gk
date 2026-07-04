package gerr

import (
	"errors"
	"testing"
)

func TestMultiErrorUnwrapsMultipleErrors(t *testing.T) {
	errName := errors.New("name required")
	errEmail := New("email invalid", WithCode("email.invalid"), WithKind(KindInvalid))

	err := NewMulti("validate user", nil, errName, errEmail)

	if !errors.Is(err, errName) {
		t.Fatal("multi error should match first child")
	}
	if !IsCode(err, "email.invalid") {
		t.Fatal("multi error should match code from child")
	}
	if !IsKind(err, KindInvalid) {
		t.Fatal("multi error should match kind from child")
	}

	var multi *MultiError
	if !errors.As(err, &multi) {
		t.Fatal("multi error should expose *MultiError")
	}
	if len(multi.Errors) != 2 {
		t.Fatalf("multi.Errors length = %d, want 2", len(multi.Errors))
	}
}

func TestFlattenReturnsLeafErrors(t *testing.T) {
	errA := errors.New("a")
	errB := errors.New("b")
	errC := errors.New("c")

	err := NewMulti("outer", Wrap(errA, "wrap a"), NewMulti("inner", errB, errC))

	leaves := Flatten(err)
	if len(leaves) != 3 {
		t.Fatalf("Flatten length = %d, want 3", len(leaves))
	}
	for _, want := range []error{errA, errB, errC} {
		if !containsExact(leaves, want) {
			t.Fatalf("Flatten missing %v", want)
		}
	}
}

func TestContainsAndIsAny(t *testing.T) {
	errA := errors.New("a")
	errB := errors.New("b")
	err := NewMulti("batch", errA, Wrap(errB, "wrap b", WithCode("b.code")))

	if !Contains(err, func(candidate error) bool {
		return IsCode(candidate, "b.code")
	}) {
		t.Fatal("Contains should find matching child")
	}
	if !IsAny(err, errors.New("missing"), errB) {
		t.Fatal("IsAny should match any target in error tree")
	}
}

func containsExact(errs []error, target error) bool {
	for _, err := range errs {
		if err == target {
			return true
		}
	}
	return false
}

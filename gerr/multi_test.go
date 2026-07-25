package gerr

import (
	"errors"
	"testing"
)

func TestMultiErrorHasExplicitTopLevelDescriptor(t *testing.T) {
	errName := errors.New("name required")
	errEmail := New("email.invalid", KindInvalid)
	multi := NewMulti("request.validation_failed", KindInvalid, []error{nil, errName, errEmail}, WithParam("source", "body"))

	if !errors.Is(multi, errName) || !errors.Is(multi, errEmail) {
		t.Fatal("multi error should match children")
	}
	descriptor, ok := Describe(multi)
	if !ok || descriptor.ID != "request.validation_failed" || descriptor.Params["source"] != "body" {
		t.Fatalf("Describe() = %#v, %v", descriptor, ok)
	}
	if len(multi.Unwrap()) != 2 {
		t.Fatalf("children = %d, want 2", len(multi.Unwrap()))
	}
}

func TestNewMultiWithoutChildrenReturnsNil(t *testing.T) {
	if got := NewMulti("request.validation_failed", KindInvalid, []error{nil}); got != nil {
		t.Fatalf("NewMulti() = %v, want nil", got)
	}
}

func TestJoinHasNoPublicDescriptor(t *testing.T) {
	joined := Join(New("user.not_found", KindNotFound), New("order.conflict", KindConflict))
	if _, ok := Describe(joined); ok {
		t.Fatal("Join should not select a child descriptor")
	}
}

func TestFlattenReturnsLeafErrors(t *testing.T) {
	errA := errors.New("a")
	errB := errors.New("b")
	errC := errors.New("c")
	err := Join(Wrap(errA, WithMessage("wrap a")), Join(errB, errC))

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

func containsExact(errs []error, target error) bool {
	for _, err := range errs {
		if err == target {
			return true
		}
	}
	return false
}

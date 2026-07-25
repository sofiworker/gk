package gerr

import (
	"errors"
	"fmt"
	"testing"
)

type testDescriber struct {
	descriptor Descriptor
	err        error
}

func (e testDescriber) Error() string               { return "described" }
func (e testDescriber) Unwrap() error               { return e.err }
func (e testDescriber) ErrorDescriptor() Descriptor { return e.descriptor }

func TestDescribeUsesOutermostValidDescriptor(t *testing.T) {
	inner := New("user.not_found", KindNotFound, WithParam("level", "inner"))
	outer := testDescriber{descriptor: Descriptor{ID: "api.user_missing", Kind: KindInvalid, Params: map[string]any{"level": "outer"}}, err: inner}

	got, ok := Describe(fmt.Errorf("transport: %w", outer))
	if !ok || got.ID != "api.user_missing" || got.Params["level"] != "outer" {
		t.Fatalf("Describe() = %#v, %v", got, ok)
	}
}

func TestDescribeSkipsInvalidDescriptorOnSingleChain(t *testing.T) {
	inner := New("user.not_found", KindNotFound)
	got, ok := Describe(testDescriber{descriptor: Descriptor{ID: "invalid"}, err: inner})
	if !ok || got.ID != "user.not_found" {
		t.Fatalf("Describe() = %#v, %v", got, ok)
	}
}

func TestDescribeStopsAtJoinedErrors(t *testing.T) {
	joined := errors.Join(New("user.not_found", KindNotFound), New("order.conflict", KindConflict))
	if got, ok := Describe(joined); ok {
		t.Fatalf("Describe(join) = %#v, true; want false", got)
	}
}

func TestDescribeReturnsDefensiveParamsCopy(t *testing.T) {
	err := New("user.not_found", KindNotFound, WithParam("nested", map[string]any{"id": 42}))
	first, ok := Describe(err)
	if !ok {
		t.Fatal("Describe() = false")
	}
	first.Params["nested"].(map[string]any)["id"] = 7
	second, _ := Describe(err)
	if second.Params["nested"].(map[string]any)["id"] != 42 {
		t.Fatal("Describe returned shared mutable Params")
	}
}

func TestValidMessageID(t *testing.T) {
	tests := map[string]bool{
		"user.not_found": true,
		"a.b":            true,
		"invalid":        false,
		"User.not_found": false,
		"user.-bad":      false,
	}
	for id, want := range tests {
		if got := ValidMessageID(id); got != want {
			t.Errorf("ValidMessageID(%q) = %v, want %v", id, got, want)
		}
	}
}

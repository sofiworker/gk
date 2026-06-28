package codegen

import (
	"strings"
	"testing"
)

func TestGenerateGServerFile(t *testing.T) {
	src, err := generateGServerFile(modelFixture(), Config{Package: "userws"})
	if err != nil {
		t.Fatalf("generateGServerFile returned error: %v", err)
	}

	if !strings.Contains(src, "func RegisterUserServiceServer") {
		t.Fatalf("expected gserver register helper, got:\n%s", src)
	}
	if !strings.Contains(src, "adaptergserver.Register") {
		t.Fatalf("expected adapter wiring, got:\n%s", src)
	}
}

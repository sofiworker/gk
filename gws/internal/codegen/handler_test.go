package codegen

import (
	"strings"
	"testing"
)

func TestGenerateHandlerFile(t *testing.T) {
	src, err := generateHandlerFile(modelFixture(), Config{Package: "userws"})
	if err != nil {
		t.Fatalf("generateHandlerFile returned error: %v", err)
	}

	if !strings.Contains(src, "type UserServiceServer interface") {
		t.Fatalf("expected server interface, got:\n%s", src)
	}
	if !strings.Contains(src, "func NewUserServiceHandler") {
		t.Fatalf("expected handler constructor, got:\n%s", src)
	}
	if !strings.Contains(src, "&gws.ServiceDesc{") {
		t.Fatalf("expected service desc wiring, got:\n%s", src)
	}
	if !strings.Contains(src, "func UserServiceDesc() *gws.ServiceDesc") {
		t.Fatalf("expected exported service desc accessor, got:\n%s", src)
	}
	if !strings.Contains(src, "Operation:   CreateUserOperation(),") {
		t.Fatalf("expected service desc to use exported operation accessor, got:\n%s", src)
	}
	if strings.Contains(src, "opCreateUser") {
		t.Fatalf("unexpected private operation variable usage in handler file, got:\n%s", src)
	}
	if !strings.Contains(src, "desc := UserServiceDesc()") {
		t.Fatalf("expected handler to reuse exported service desc accessor, got:\n%s", src)
	}
}

func TestGenerateHandlerFileWithoutEmbeddedWSDL(t *testing.T) {
	src, err := generateHandlerFile(modelFixture(), Config{
		Package:             "userws",
		EmbedWSDL:           false,
		ExplicitOutputFlags: true,
	})
	if err != nil {
		t.Fatalf("generateHandlerFile returned error: %v", err)
	}

	if strings.Contains(src, "WSDL:") {
		t.Fatalf("unexpected embedded wsdl reference, got:\n%s", src)
	}
}

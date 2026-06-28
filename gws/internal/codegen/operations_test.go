package codegen

import (
	"strings"
	"testing"
)

func TestGenerateOperationsFile(t *testing.T) {
	src, err := generateOperationsFile(modelFixture(), Config{Package: "userws"})
	if err != nil {
		t.Fatalf("generateOperationsFile returned error: %v", err)
	}

	if !strings.Contains(src, "var opCreateUser = gws.Operation{") {
		t.Fatalf("expected operation literal, got:\n%s", src)
	}
	if !strings.Contains(src, `RequestWrapper:`) || !strings.Contains(src, `Local: "CreateUserRequest"`) {
		t.Fatalf("expected request wrapper metadata, got:\n%s", src)
	}
	if !strings.Contains(src, `ResponseWrapper:`) || !strings.Contains(src, `Local: "CreateUserResponse"`) {
		t.Fatalf("expected response wrapper metadata, got:\n%s", src)
	}
	if strings.Count(src, `Space: "urn:user/types"`) < 2 {
		t.Fatalf("expected wrapper namespace metadata, got:\n%s", src)
	}
	if !strings.Contains(src, `"urn:user:CreateUser"`) {
		t.Fatalf("expected soap action, got:\n%s", src)
	}
	if !strings.Contains(src, "func CreateUserOperation() gws.Operation") {
		t.Fatalf("expected exported operation accessor, got:\n%s", src)
	}
	if !strings.Contains(src, "return opCreateUser") {
		t.Fatalf("expected exported operation accessor body, got:\n%s", src)
	}
}

func TestGenerateOperationsFileSelectedServicePort(t *testing.T) {
	src, err := generateOperationsFile(multiServiceFixture(), Config{
		Package: "userws",
		Service: "AdminService",
		Port:    "AdminPort",
	})
	if err != nil {
		t.Fatalf("generateOperationsFile returned error: %v", err)
	}

	if !strings.Contains(src, "var opDeleteUser = gws.Operation{") {
		t.Fatalf("expected selected operation, got:\n%s", src)
	}
	if strings.Contains(src, "opCreateUser") {
		t.Fatalf("unexpected non-selected operation, got:\n%s", src)
	}
}

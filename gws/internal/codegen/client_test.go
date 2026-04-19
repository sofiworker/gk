package codegen

import (
	"strings"
	"testing"
)

func TestGenerateClientFile(t *testing.T) {
	src, err := generateClientFile(modelFixture(), Config{Package: "userws"})
	if err != nil {
		t.Fatalf("generateClientFile returned error: %v", err)
	}

	if !strings.Contains(src, "type UserServiceClient struct") {
		t.Fatalf("expected client type, got:\n%s", src)
	}
	if !strings.Contains(src, "func (c *UserServiceClient) NewCreateUserRequest") {
		t.Fatalf("expected request constructor method, got:\n%s", src)
	}
	if !strings.Contains(src, "func (c *UserServiceClient) CreateUser") {
		t.Fatalf("expected operation method, got:\n%s", src)
	}
	if !strings.Contains(src, "gws.NewRequest") {
		t.Fatalf("expected request builder integration, got:\n%s", src)
	}
	if !strings.Contains(src, "gws.NewRequest(ctx, c.endpoint, CreateUserOperation())") {
		t.Fatalf("expected request builder to use exported operation accessor, got:\n%s", src)
	}
	if strings.Contains(src, "opCreateUser") {
		t.Fatalf("unexpected private operation variable usage in client file, got:\n%s", src)
	}
	if !strings.Contains(src, "func (c *UserServiceClient) Client() *gws.Client") {
		t.Fatalf("expected low-level client accessor, got:\n%s", src)
	}
	if !strings.Contains(src, "func (c *UserServiceClient) Endpoint() string") {
		t.Fatalf("expected endpoint accessor, got:\n%s", src)
	}
	if !strings.Contains(src, "func (c *UserServiceClient) SetEndpoint(endpoint string) *UserServiceClient") {
		t.Fatalf("expected endpoint mutator, got:\n%s", src)
	}
	if !strings.Contains(src, "func (c *UserServiceClient) CreateUserRaw") {
		t.Fatalf("expected raw operation method, got:\n%s", src)
	}
	if !strings.Contains(src, "return c.client.DoRaw(req)") {
		t.Fatalf("expected raw operation to delegate to gws client, got:\n%s", src)
	}
}

func TestGenerateClientFileSelectedServicePort(t *testing.T) {
	src, err := generateClientFile(multiServiceFixture(), Config{
		Package: "userws",
		Service: "AdminService",
		Port:    "AdminPort",
	})
	if err != nil {
		t.Fatalf("generateClientFile returned error: %v", err)
	}

	if !strings.Contains(src, "type AdminServiceClient struct") {
		t.Fatalf("expected selected service client, got:\n%s", src)
	}
	if !strings.Contains(src, "func (c *AdminServiceClient) DeleteUser") {
		t.Fatalf("expected selected operation, got:\n%s", src)
	}
	if strings.Contains(src, "CreateUser") {
		t.Fatalf("unexpected non-selected operation, got:\n%s", src)
	}
}

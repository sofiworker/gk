package codegen

import (
	"strings"
	"testing"
)

func TestGenerateTypesFile(t *testing.T) {
	src, err := generateTypesFile(modelFixture(), Config{Package: "userws"})
	if err != nil {
		t.Fatalf("generateTypesFile returned error: %v", err)
	}

	if !strings.Contains(src, "type CreateUserRequest struct") {
		t.Fatalf("expected request wrapper struct, got:\n%s", src)
	}
	if !strings.Contains(src, `xml:"urn:user/types CreateUserRequest"`) {
		t.Fatalf("expected request xml wrapper tag, got:\n%s", src)
	}
	if !strings.Contains(src, "Name string") {
		t.Fatalf("expected request fields, got:\n%s", src)
	}
}

func TestGoTypeForXSD(t *testing.T) {
	tests := []struct {
		name     string
		xsdType  string
		optional bool
		repeated bool
		want     string
	}{
		{
			name:    "string",
			xsdType: "string",
			want:    "string",
		},
		{
			name:     "optional bool",
			xsdType:  "boolean",
			optional: true,
			want:     "*bool",
		},
		{
			name:     "repeated int",
			xsdType:  "int",
			repeated: true,
			want:     "[]int32",
		},
		{
			name:    "datetime",
			xsdType: "dateTime",
			want:    "time.Time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goTypeForXSD(tt.xsdType, tt.optional, tt.repeated); got != tt.want {
				t.Fatalf("unexpected go type: got=%q want=%q", got, tt.want)
			}
		})
	}
}

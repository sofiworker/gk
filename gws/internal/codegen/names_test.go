package codegen

import "testing"

func TestTypeNamePascalCase(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		prefix string
		want   string
	}{
		{
			name:  "snake and dash",
			input: "create-user_request",
			want:  "CreateUserRequest",
		},
		{
			name:   "prefix applied",
			input:  "request",
			prefix: "User",
			want:   "UserRequest",
		},
		{
			name:  "collapse spaces",
			input: "create user response",
			want:  "CreateUserResponse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := typeName(tt.input, tt.prefix); got != tt.want {
				t.Fatalf("unexpected type name: got=%q want=%q", got, tt.want)
			}
		})
	}
}

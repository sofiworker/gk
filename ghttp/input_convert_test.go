package ghttp

import (
	"errors"
	"testing"
)

func TestToInt(t *testing.T) {
	tests := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{"42", 42, false},
		{"-7", -7, false},
		{"0", 0, false},
		{"abc", 0, true},
		{"", 0, true},
		{"3.14", 0, true},
	}
	for _, tt := range tests {
		got, err := toInt("query", "n", tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Errorf("toInt(%q): expected error", tt.raw)
			} else if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("toInt(%q): err = %v, want wrapping ErrInvalidInput", tt.raw, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("toInt(%q): unexpected err %v", tt.raw, err)
		}
		if got != tt.want {
			t.Errorf("toInt(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestToInt64(t *testing.T) {
	got, err := toInt64("query", "n", "9223372036854775807")
	if err != nil || got != 9223372036854775807 {
		t.Errorf("toInt64 maxint = %d, err=%v", got, err)
	}
	if _, err := toInt64("query", "n", "notanumber"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("toInt64 invalid: err = %v, want ErrInvalidInput", err)
	}
}

func TestToBool(t *testing.T) {
	truthy := []string{"1", "true", "t", "yes", "y", "on"}
	for _, s := range truthy {
		got, err := toBool("query", "b", s)
		if err != nil || !got {
			t.Errorf("toBool(%q) = %v, err=%v, want true", s, got, err)
		}
	}
	falsy := []string{"0", "false", "f", "no", "n", "off"}
	for _, s := range falsy {
		got, err := toBool("query", "b", s)
		if err != nil || got {
			t.Errorf("toBool(%q) = %v, err=%v, want false", s, got, err)
		}
	}
	if _, err := toBool("query", "b", "maybe"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("toBool(maybe): err = %v, want ErrInvalidInput", err)
	}
}

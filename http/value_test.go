package ghttp

import (
	"net/http"
	"net/url"
	"testing"
)

func TestValue_Int(t *testing.T) {
	tests := []struct {
		name      string
		value     *Value
		want      int
		wantError bool
	}{
		{
			name:      "valid int",
			value:     newValue("42", true),
			want:      42,
			wantError: false,
		},
		{
			name:      "negative int",
			value:     newValue("-100", true),
			want:      -100,
			wantError: false,
		},
		{
			name:      "empty string",
			value:     newValue("", true),
			want:      0,
			wantError: true,
		},
		{
			name:      "invalid int",
			value:     newValue("abc", true),
			want:      0,
			wantError: true,
		},
		{
			name:      "not exists",
			value:     newValue("", false),
			want:      0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.value.Int()
			if (err != nil) != tt.wantError {
				t.Errorf("Int() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if got != tt.want {
				t.Errorf("Int() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValue_Int64(t *testing.T) {
	tests := []struct {
		name      string
		value     *Value
		want      int64
		wantError bool
	}{
		{
			name:      "valid int64",
			value:     newValue("9223372036854775807", true),
			want:      9223372036854775807,
			wantError: false,
		},
		{
			name:      "negative int64",
			value:     newValue("-9223372036854775808", true),
			want:      -9223372036854775808,
			wantError: false,
		},
		{
			name:      "empty string",
			value:     newValue("", true),
			want:      0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.value.Int64()
			if (err != nil) != tt.wantError {
				t.Errorf("Int64() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if got != tt.want {
				t.Errorf("Int64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValue_Float64(t *testing.T) {
	tests := []struct {
		name      string
		value     *Value
		want      float64
		wantError bool
	}{
		{
			name:      "valid float",
			value:     newValue("3.14", true),
			want:      3.14,
			wantError: false,
		},
		{
			name:      "negative float",
			value:     newValue("-2.5", true),
			want:      -2.5,
			wantError: false,
		},
		{
			name:      "integer as float",
			value:     newValue("42", true),
			want:      42.0,
			wantError: false,
		},
		{
			name:      "empty string",
			value:     newValue("", true),
			want:      0,
			wantError: true,
		},
		{
			name:      "invalid float",
			value:     newValue("abc", true),
			want:      0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.value.Float64()
			if (err != nil) != tt.wantError {
				t.Errorf("Float64() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if got != tt.want {
				t.Errorf("Float64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValue_Bool(t *testing.T) {
	tests := []struct {
		name      string
		value     *Value
		want      bool
		wantError bool
	}{
		{
			name:      "true",
			value:     newValue("true", true),
			want:      true,
			wantError: false,
		},
		{
			name:      "false",
			value:     newValue("false", true),
			want:      false,
			wantError: false,
		},
		{
			name:      "1 as true",
			value:     newValue("1", true),
			want:      true,
			wantError: false,
		},
		{
			name:      "0 as false",
			value:     newValue("0", true),
			want:      false,
			wantError: false,
		},
		{
			name:      "empty string as false",
			value:     newValue("", true),
			want:      false,
			wantError: false,
		},
		{
			name:      "invalid bool",
			value:     newValue("abc", true),
			want:      false,
			wantError: true,
		},
		{
			name:      "not exists",
			value:     newValue("", false),
			want:      false,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.value.Bool()
			if (err != nil) != tt.wantError {
				t.Errorf("Bool() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if got != tt.want {
				t.Errorf("Bool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValue_IntOr(t *testing.T) {
	tests := []struct {
		name         string
		value        *Value
		defaultValue int
		want         int
	}{
		{
			name:         "valid int",
			value:        newValue("42", true),
			defaultValue: 10,
			want:         42,
		},
		{
			name:         "invalid int uses default",
			value:        newValue("abc", true),
			defaultValue: 10,
			want:         10,
		},
		{
			name:         "not exists uses default",
			value:        newValue("", false),
			defaultValue: 10,
			want:         10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.value.IntOr(tt.defaultValue)
			if got != tt.want {
				t.Errorf("IntOr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValue_Exists(t *testing.T) {
	tests := []struct {
		name  string
		value *Value
		want  bool
	}{
		{
			name:  "exists with value",
			value: newValue("test", true),
			want:  true,
		},
		{
			name:  "exists with empty string",
			value: newValue("", true),
			want:  true,
		},
		{
			name:  "not exists",
			value: newValue("", false),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Exists(); got != tt.want {
				t.Errorf("Exists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValues_IntSlice(t *testing.T) {
	tests := []struct {
		name      string
		values    *Values
		want      []int
		wantError bool
	}{
		{
			name:      "valid ints",
			values:    newValues([]string{"1", "2", "3"}),
			want:      []int{1, 2, 3},
			wantError: false,
		},
		{
			name:      "empty slice",
			values:    newValues([]string{}),
			want:      []int{},
			wantError: false,
		},
		{
			name:      "invalid int in slice",
			values:    newValues([]string{"1", "abc", "3"}),
			want:      nil,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.values.IntSlice()
			if (err != nil) != tt.wantError {
				t.Errorf("IntSlice() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if !tt.wantError && len(got) != len(tt.want) {
				t.Errorf("IntSlice() length = %v, want %v", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("IntSlice()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPathValue(t *testing.T) {
	req := &Request{
		Params: Params{
			{Key: "id", Value: "123"},
			{Key: "name", Value: "test"},
		},
	}

	t.Run("existing path parameter", func(t *testing.T) {
		val := PathValue(req, "id")
		if !val.Exists() {
			t.Error("PathValue() should exist")
		}
		got, err := val.String()
		if err != nil {
			t.Errorf("String() error = %v", err)
		}
		if got != "123" {
			t.Errorf("String() = %v, want %v", got, "123")
		}
	})

	t.Run("non-existing path parameter", func(t *testing.T) {
		val := PathValue(req, "unknown")
		if val.Exists() {
			t.Error("PathValue() should not exist")
		}
	})
}

func TestQueryValue(t *testing.T) {
	rawReq := &http.Request{
		URL: &url.URL{
			RawQuery: "page=1&limit=20&tags=go&tags=rust",
		},
	}
	req := &Request{Raw: rawReq}

	t.Run("existing query parameter", func(t *testing.T) {
		val := QueryValue(req, "page")
		if !val.Exists() {
			t.Error("QueryValue() should exist")
		}
		got, err := val.Int()
		if err != nil {
			t.Errorf("Int() error = %v", err)
		}
		if got != 1 {
			t.Errorf("Int() = %v, want %v", got, 1)
		}
	})

	t.Run("non-existing query parameter", func(t *testing.T) {
		val := QueryValue(req, "unknown")
		if val.Exists() {
			t.Error("QueryValue() should not exist")
		}
	})
}

func TestQueryValues(t *testing.T) {
	rawReq := &http.Request{
		URL: &url.URL{
			RawQuery: "tags=go&tags=rust&tags=python",
		},
	}
	req := &Request{Raw: rawReq}

	t.Run("multi-value query parameter", func(t *testing.T) {
		vals := QueryValues(req, "tags")
		if vals.Len() != 3 {
			t.Errorf("Len() = %v, want %v", vals.Len(), 3)
		}
		strs := vals.Strings()
		want := []string{"go", "rust", "python"}
		for i, s := range strs {
			if s != want[i] {
				t.Errorf("Strings()[%d] = %v, want %v", i, s, want[i])
			}
		}
	})

	t.Run("non-existing query parameter", func(t *testing.T) {
		vals := QueryValues(req, "unknown")
		if vals.Len() != 0 {
			t.Errorf("Len() = %v, want %v", vals.Len(), 0)
		}
	})
}

func TestHeaderValue(t *testing.T) {
	rawReq := &http.Request{
		Header: http.Header{
			"Authorization": []string{"Bearer token123"},
			"Content-Type":  []string{"application/json"},
		},
	}
	req := &Request{Raw: rawReq}

	t.Run("existing header", func(t *testing.T) {
		val := HeaderValue(req, "Authorization")
		if !val.Exists() {
			t.Error("HeaderValue() should exist")
		}
		got, err := val.String()
		if err != nil {
			t.Errorf("String() error = %v", err)
		}
		if got != "Bearer token123" {
			t.Errorf("String() = %v, want %v", got, "Bearer token123")
		}
	})

	t.Run("non-existing header", func(t *testing.T) {
		val := HeaderValue(req, "X-Custom")
		if val.Exists() {
			t.Error("HeaderValue() should not exist")
		}
	})
}

func TestCookieValue(t *testing.T) {
	rawReq := &http.Request{
		Header: http.Header{
			"Cookie": []string{"session=abc123; user=test"},
		},
	}
	req := &Request{Raw: rawReq}

	t.Run("existing cookie", func(t *testing.T) {
		val := CookieValue(req, "session")
		if !val.Exists() {
			t.Error("CookieValue() should exist")
		}
		got, err := val.String()
		if err != nil {
			t.Errorf("String() error = %v", err)
		}
		if got != "abc123" {
			t.Errorf("String() = %v, want %v", got, "abc123")
		}
	})

	t.Run("non-existing cookie", func(t *testing.T) {
		val := CookieValue(req, "unknown")
		if val.Exists() {
			t.Error("CookieValue() should not exist")
		}
	})
}

func TestRequestInput_ValueMethods(t *testing.T) {
	rawReq := &http.Request{
		URL: &url.URL{
			RawQuery: "page=1",
		},
		Header: http.Header{
			"Authorization": []string{"Bearer token"},
		},
	}
	req := &Request{
		Raw: rawReq,
		Params: Params{
			{Key: "id", Value: "42"},
		},
	}
	input := RequestInput{req: req}

	t.Run("PathValue", func(t *testing.T) {
		val := input.PathValue("id")
		got, _ := val.Int()
		if got != 42 {
			t.Errorf("PathValue().Int() = %v, want %v", got, 42)
		}
	})

	t.Run("QueryValue", func(t *testing.T) {
		val := input.QueryValue("page")
		got, _ := val.Int()
		if got != 1 {
			t.Errorf("QueryValue().Int() = %v, want %v", got, 1)
		}
	})

	t.Run("HeaderValue", func(t *testing.T) {
		val := input.HeaderValue("Authorization")
		got, _ := val.String()
		if got != "Bearer token" {
			t.Errorf("HeaderValue().String() = %v, want %v", got, "Bearer token")
		}
	})
}

func TestValueMustMethods(t *testing.T) {
	valid := newNamedValue("id", "42", true)
	if valid.MustString() != "42" || valid.MustInt() != 42 || valid.MustInt64() != 42 || valid.MustFloat64() != 42 {
		t.Fatal("unexpected Must conversion result")
	}
	if !newNamedValue("enabled", "true", true).MustBool() {
		t.Fatal("MustBool returned false for true")
	}
	for name, value := range map[string]Value{
		"missing string": {name: "id"},
		"invalid int":    {name: "id", raw: "x", exists: true},
		"invalid int64":  {name: "id", raw: "x", exists: true},
		"invalid float":  {name: "id", raw: "x", exists: true},
		"invalid bool":   {name: "id", raw: "x", exists: true},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Must method did not panic")
				}
			}()
			switch name {
			case "missing string":
				value.MustString()
			case "invalid int":
				value.MustInt()
			case "invalid int64":
				value.MustInt64()
			case "invalid float":
				value.MustFloat64()
			case "invalid bool":
				value.MustBool()
			}
		})
	}
}

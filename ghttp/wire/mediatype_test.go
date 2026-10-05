package wire

import "testing"

// TestMediaType 覆盖参数、大小写、空白与空串。
// TestMediaType covers parameters, case, whitespace and the empty string.
func TestMediaType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "application/json", "application/json"},
		{"with charset", "application/json; charset=utf-8", "application/json"},
		{"no space before semicolon", "text/plain;charset=utf-8", "text/plain"},
		{"upper case", "Application/JSON", "application/json"},
		{"surrounding whitespace", "  application/xml  ", "application/xml"},
		{"whitespace before param", "multipart/form-data ; boundary=x", "multipart/form-data"},
		{"empty", "", ""},
		{"only whitespace", "   ", ""},
		{"only params", "; charset=utf-8", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MediaType(tt.in); got != tt.want {
				t.Fatalf("MediaType(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestContentTypeIn 覆盖空放行、命中与不命中。
// TestContentTypeIn covers empty pass-through, hit and miss.
func TestContentTypeIn(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want []string
		ok   bool
	}{
		{"empty got passes", "", []string{ContentTypeJSON}, true},
		{"empty want passes", "text/html", nil, true},
		{"empty want slice passes", "text/html", []string{}, true},
		{"both empty", "", nil, true},
		{"hit", "application/json", []string{ContentTypeXML, ContentTypeJSON}, true},
		{"hit with params and case", "Application/JSON; charset=utf-8", []string{ContentTypeJSON}, true},
		{"miss", "text/html", []string{ContentTypeJSON, ContentTypeXML}, false},
		{"miss on prefix only", "application/jsonx", []string{ContentTypeJSON}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ok := ContentTypeIn(tt.got, tt.want); ok != tt.ok {
				t.Fatalf("ContentTypeIn(%q, %v) = %v, want %v", tt.got, tt.want, ok, tt.ok)
			}
		})
	}
}

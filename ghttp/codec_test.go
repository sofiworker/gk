package ghttp

import (
	"net/http"
	"testing"
)

func TestSetContentTypeHeaderUsesSharedFrameworkValues(t *testing.T) {
	tests := []struct {
		contentType string
		want        []string
	}{
		{"application/json; charset=utf-8", jsonContentTypeHeader},
		{"application/xml; charset=utf-8", xmlContentTypeHeader},
		{"text/plain; charset=utf-8", textContentTypeHeader},
		{"application/custom", []string{"application/custom"}},
	}
	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			h := make(http.Header)
			setContentTypeHeader(h, tt.contentType)
			if got := h["Content-Type"]; len(got) != 1 || got[0] != tt.contentType {
				t.Fatalf("Content-Type = %#v, want %q", got, tt.contentType)
			}
			if tt.contentType != "application/custom" && &h["Content-Type"][0] != &tt.want[0] {
				t.Fatalf("framework content type %q did not use the shared value", tt.contentType)
			}
		})
	}
}

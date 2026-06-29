package ghttp

import (
	"testing"
)

func TestMIMEConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"MIMEJSON", MIMEJSON, "application/json"},
		{"MIMEHTML", MIMEHTML, "text/html"},
		{"MIMEXML", MIMEXML, "application/xml"},
		{"MIMEXML2", MIMEXML2, "text/xml"},
		{"MIMEPlain", MIMEPlain, "text/plain"},
		{"MIMEPOSTForm", MIMEPOSTForm, "application/x-www-form-urlencoded"},
		{"MIMEMultipartPOSTForm", MIMEMultipartPOSTForm, "multipart/form-data"},
		{"MIMEYAML", MIMEYAML, "application/x-yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, tt.got)
			}
		})
	}
}

func TestConstantsAccessible(t *testing.T) {
	_ = MIMEJSON
	_ = MIMEHTML
	_ = MIMEXML
	_ = MIMEPlain
	_ = MIMEPOSTForm
	_ = MIMEMultipartPOSTForm
}

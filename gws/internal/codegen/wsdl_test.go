package codegen

import (
	"strings"
	"testing"
)

func TestGenerateWSDLFile(t *testing.T) {
	src, err := generateWSDLFile(modelFixture(), Config{
		Package: "userws",
		WSDL:    []byte("<definitions/>"),
		XSD: map[string][]byte{
			"types.xsd": []byte("<schema/>"),
		},
	})
	if err != nil {
		t.Fatalf("generateWSDLFile returned error: %v", err)
	}

	if !strings.Contains(src, "var userServiceWSDL = &gws.WSDLAssetSet{") {
		t.Fatalf("expected wsdl asset variable, got:\n%s", src)
	}
	if !strings.Contains(src, `"types.xsd": []byte("<schema/>")`) {
		t.Fatalf("expected xsd asset embedding, got:\n%s", src)
	}
}

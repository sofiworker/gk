package wsdl

import "testing"

func TestLoadLocalImports(t *testing.T) {
	data := loadFixture(t, "echo.wsdl")
	baseDir := fixturePath(t, "")

	m, err := Parse(data, baseDir)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	schema := findSchemaByNamespace(t, m.Schemas, "urn:echo/types")
	if len(schema.Elements) == 0 {
		t.Fatal("expected imported schema elements to be loaded")
	}
	_ = findElementByName(t, schema.Elements, "EchoRequest")
	_ = findComplexTypeByName(t, schema.ComplexTypes, "EchoResponseType")
}

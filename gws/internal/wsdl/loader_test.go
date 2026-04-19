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

func TestLoadLocalAssets(t *testing.T) {
	wsdlPath := fixturePath(t, "echo.wsdl")

	mainData, xsdAssets, err := LoadLocalAssets(wsdlPath)
	if err != nil {
		t.Fatalf("LoadLocalAssets returned error: %v", err)
	}

	if len(mainData) == 0 {
		t.Fatal("expected wsdl main data")
	}
	if len(xsdAssets) != 1 {
		t.Fatalf("unexpected xsd asset count: %d", len(xsdAssets))
	}
	if string(xsdAssets["echo.xsd"]) == "" {
		t.Fatal("expected echo.xsd asset")
	}
}

package codegen

import (
	"fmt"
	"strings"

	"github.com/sofiworker/gk/gws/internal/model"
)

const xmlSchemaNamespace = "http://www.w3.org/2001/XMLSchema"

func generateTypesFile(m model.Model, cfg Config) (string, error) {
	var body strings.Builder

	index := buildTypeIndex(m)
	writeSimpleTypes(&body, m, cfg, index)
	writeComplexTypes(&body, m, cfg, index)
	writeElementTypes(&body, m, cfg, index)

	imports := collectTypeImports(m)
	source, err := renderGoFile(cfg.Package, imports, body.String())
	if err != nil {
		return "", err
	}

	return string(source), nil
}

func goTypeForXSD(xsdType string, optional bool, repeated bool) string {
	base := mapXSDType(strings.TrimSpace(xsdType))
	if base == "" {
		base = "string"
	}

	return applyTypeModifiers(base, optional, repeated)
}

type typeIndex struct {
	complex map[string]model.ComplexType
	simple  map[string]model.SimpleType
}

func buildTypeIndex(m model.Model) typeIndex {
	index := typeIndex{
		complex: make(map[string]model.ComplexType),
		simple:  make(map[string]model.SimpleType),
	}

	for _, schema := range m.Schemas {
		for _, complexType := range schema.ComplexTypes {
			name := model.QName{Space: schema.TargetNamespace, Local: complexType.Name}
			index.complex[qnameKey(name)] = complexType
		}
		for _, simpleType := range schema.SimpleTypes {
			name := model.QName{Space: schema.TargetNamespace, Local: simpleType.Name}
			index.simple[qnameKey(name)] = simpleType
		}
	}

	return index
}

func writeSimpleTypes(body *strings.Builder, m model.Model, cfg Config, index typeIndex) {
	for _, schema := range m.Schemas {
		for _, simpleType := range schema.SimpleTypes {
			goName := typeName(simpleType.Name, cfg.TypePrefix)
			base := goTypeForQName(simpleType.Base, false, false, cfg.TypePrefix, index)
			if base == "" {
				base = "string"
			}

			body.WriteString(fmt.Sprintf("type %s %s\n\n", goName, base))
		}
	}
}

func writeComplexTypes(body *strings.Builder, m model.Model, cfg Config, index typeIndex) {
	for _, schema := range m.Schemas {
		for _, complexType := range schema.ComplexTypes {
			goName := typeName(complexType.Name, cfg.TypePrefix)
			writeStructType(body, goName, model.QName{}, complexType.Fields, cfg, index)
		}
	}
}

func writeElementTypes(body *strings.Builder, m model.Model, cfg Config, index typeIndex) {
	for _, schema := range m.Schemas {
		for _, elem := range schema.Elements {
			goName := typeName(elem.Name, cfg.TypePrefix)
			fields := fieldsForElement(elem, index)
			writeStructType(body, goName, model.QName{Space: schema.TargetNamespace, Local: elem.Name}, fields, cfg, index)
		}
	}
}

func fieldsForElement(elem model.Element, index typeIndex) []model.Field {
	if complexType, ok := index.complex[qnameKey(elem.Type)]; ok {
		return complexType.Fields
	}

	return nil
}

func writeStructType(
	body *strings.Builder,
	goName string,
	xmlName model.QName,
	fields []model.Field,
	cfg Config,
	index typeIndex,
) {
	body.WriteString(fmt.Sprintf("type %s struct {\n", goName))
	if xmlName.Local != "" {
		body.WriteString(fmt.Sprintf("\tXMLName xml.Name `xml:\"%s %s\"`\n", xmlName.Space, xmlName.Local))
	}
	for _, field := range fields {
		fieldName := typeName(field.Name, "")
		fieldType := goTypeForQName(field.Type, isOptionalField(field), isRepeatedField(field), cfg.TypePrefix, index)
		tag := field.Name
		if isOptionalField(field) || isRepeatedField(field) {
			tag += ",omitempty"
		}

		body.WriteString(fmt.Sprintf("\t%s %s `xml:\"%s\"`\n", fieldName, fieldType, tag))
	}
	body.WriteString("}\n\n")
}

func goTypeForQName(name model.QName, optional bool, repeated bool, prefix string, index typeIndex) string {
	if name.Space == xmlSchemaNamespace || name.Space == "" {
		if base := mapXSDType(name.Local); base != "" {
			return applyTypeModifiers(base, optional, repeated)
		}
	}

	base := typeName(name.Local, prefix)
	if base == "" {
		base = "string"
	}

	if _, ok := index.simple[qnameKey(name)]; ok {
		return applyTypeModifiers(base, optional, repeated)
	}
	if _, ok := index.complex[qnameKey(name)]; ok {
		return applyTypeModifiers(base, optional, repeated)
	}

	return applyTypeModifiers(base, optional, repeated)
}

func mapXSDType(name string) string {
	switch strings.TrimSpace(name) {
	case "string", "token", "normalizedString":
		return "string"
	case "boolean":
		return "bool"
	case "byte":
		return "int8"
	case "short":
		return "int16"
	case "int", "integer":
		return "int32"
	case "long":
		return "int64"
	case "float":
		return "float32"
	case "double":
		return "float64"
	case "decimal":
		return "string"
	case "date", "time", "dateTime":
		return "time.Time"
	case "base64Binary":
		return "[]byte"
	default:
		return ""
	}
}

func applyTypeModifiers(base string, optional bool, repeated bool) string {
	if repeated {
		if strings.HasPrefix(base, "[]") {
			return base
		}
		return "[]" + base
	}
	if optional && !strings.HasPrefix(base, "[]") {
		return "*" + base
	}
	return base
}

func isOptionalField(field model.Field) bool {
	return field.MinOccurs == 0 || field.Nillable
}

func isRepeatedField(field model.Field) bool {
	return field.MaxOccurs == model.MaxOccursUnbounded || field.MaxOccurs > 1
}

func collectTypeImports(m model.Model) []string {
	var imports []string
	if hasElementTypes(m) {
		imports = append(imports, "encoding/xml")
	}
	if usesTimeTypes(m) {
		imports = append(imports, "time")
	}
	return imports
}

func hasElementTypes(m model.Model) bool {
	for _, schema := range m.Schemas {
		if len(schema.Elements) > 0 {
			return true
		}
	}

	return false
}

func usesTimeTypes(m model.Model) bool {
	for _, schema := range m.Schemas {
		for _, simpleType := range schema.SimpleTypes {
			if mapXSDType(simpleType.Base.Local) == "time.Time" {
				return true
			}
		}
		for _, complexType := range schema.ComplexTypes {
			for _, field := range complexType.Fields {
				if mapXSDType(field.Type.Local) == "time.Time" {
					return true
				}
			}
		}
	}

	return false
}

func qnameKey(name model.QName) string {
	return name.Space + "|" + name.Local
}

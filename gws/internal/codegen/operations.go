package codegen

import (
	"fmt"
	"strings"

	"github.com/sofiworker/gk/gws/internal/model"
)

func generateOperationsFile(m model.Model, cfg Config) (string, error) {
	selected, err := selectModel(m, cfg)
	if err != nil {
		return "", err
	}

	var body strings.Builder
	seen := make(map[string]struct{})

	for _, binding := range selected.Bindings {
		for _, operation := range binding.Operations {
			if _, ok := seen[operation.Name]; ok {
				continue
			}

			requestWrapper, responseWrapper, err := findOperationWrappers(selected, operation)
			if err != nil {
				return "", err
			}

			body.WriteString(fmt.Sprintf("var op%s = gws.Operation{\n", typeName(operation.Name, "")))
			body.WriteString(fmt.Sprintf("\tName:            %q,\n", operation.Name))
			body.WriteString(fmt.Sprintf("\tAction:          %q,\n", operation.Action))
			body.WriteString(fmt.Sprintf("\tRequestWrapper:  xml.Name{Space: %q, Local: %q},\n", requestWrapper.Space, requestWrapper.Local))
			body.WriteString(fmt.Sprintf("\tResponseWrapper: xml.Name{Space: %q, Local: %q},\n", responseWrapper.Space, responseWrapper.Local))
			body.WriteString("\tSOAPVersion:     gws.SOAP11,\n")
			body.WriteString("}\n\n")
			body.WriteString(fmt.Sprintf("func %sOperation() gws.Operation {\n", typeName(operation.Name, "")))
			body.WriteString(fmt.Sprintf("\treturn op%s\n", typeName(operation.Name, "")))
			body.WriteString("}\n\n")

			seen[operation.Name] = struct{}{}
		}
	}

	source, err := renderGoFile(cfg.Package, []string{"encoding/xml", "github.com/sofiworker/gk/gws"}, body.String())
	if err != nil {
		return "", err
	}

	return string(source), nil
}

func findOperationWrappers(m model.Model, operation model.BindingOperation) (model.QName, model.QName, error) {
	inputMessage, err := findMessage(m, operation.InputMessage)
	if err != nil {
		return model.QName{}, model.QName{}, err
	}
	outputMessage, err := findMessage(m, operation.OutputMessage)
	if err != nil {
		return model.QName{}, model.QName{}, err
	}

	requestWrapper, err := firstMessageElement(inputMessage)
	if err != nil {
		return model.QName{}, model.QName{}, err
	}
	responseWrapper, err := firstMessageElement(outputMessage)
	if err != nil {
		return model.QName{}, model.QName{}, err
	}

	return requestWrapper, responseWrapper, nil
}

func findMessage(m model.Model, name model.QName) (model.Message, error) {
	for _, message := range m.Messages {
		if message.Name == name.Local {
			return message, nil
		}
	}

	return model.Message{}, fmt.Errorf("message %q not found", name.Local)
}

func firstMessageElement(message model.Message) (model.QName, error) {
	if len(message.Parts) == 0 {
		return model.QName{}, fmt.Errorf("message %q has no parts", message.Name)
	}
	if message.Parts[0].Element.Local == "" {
		return model.QName{}, fmt.Errorf("message %q part has no element", message.Name)
	}

	return message.Parts[0].Element, nil
}

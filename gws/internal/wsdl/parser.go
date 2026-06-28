package wsdl

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sofiworker/gk/gws/internal/model"
)

const (
	wsdlNamespace = "http://schemas.xmlsoap.org/wsdl/"
	xsdNamespace  = "http://www.w3.org/2001/XMLSchema"
	soapNamespace = "http://schemas.xmlsoap.org/wsdl/soap/"
)

type xmlNode struct {
	Name     xml.Name
	Attr     []xml.Attr
	Children []*xmlNode
	NS       map[string]string
}

type portTypeOperation struct {
	inputMessage  model.QName
	outputMessage model.QName
}

type wsdlParser struct {
	baseDir string
	loader  *localLoader
}

func ParseFile(path string) (model.Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Model{}, fmt.Errorf("read wsdl file %q: %w", path, err)
	}

	return Parse(data, filepath.Dir(path))
}

func Parse(data []byte, baseDir string) (model.Model, error) {
	root, err := parseXMLNode(data)
	if err != nil {
		return model.Model{}, fmt.Errorf("parse wsdl xml: %w", err)
	}

	if root.Name.Local != "definitions" {
		return model.Model{}, fmt.Errorf("invalid wsdl root element: %s", root.Name.Local)
	}
	if root.Name.Space != "" && root.Name.Space != wsdlNamespace {
		return model.Model{}, fmt.Errorf("unsupported wsdl namespace: %s", root.Name.Space)
	}

	parser := wsdlParser{
		baseDir: baseDir,
		loader:  newLocalLoader(),
	}

	return parser.parseDefinitions(root)
}

func (p wsdlParser) parseDefinitions(root *xmlNode) (model.Model, error) {
	targetNamespace := attr(root, "targetNamespace")
	m := model.Model{TargetNamespace: targetNamespace}

	schemas, err := p.parseTypes(root)
	if err != nil {
		return model.Model{}, err
	}
	m.Schemas = mergeSchemasByTargetNamespace(schemas)

	portTypeOperations, err := parsePortTypeOperations(root, targetNamespace)
	if err != nil {
		return model.Model{}, err
	}

	m.Messages, err = parseMessages(root, targetNamespace)
	if err != nil {
		return model.Model{}, err
	}

	m.Bindings, err = parseBindings(root, targetNamespace, portTypeOperations)
	if err != nil {
		return model.Model{}, err
	}

	m.Services, err = parseServices(root, targetNamespace)
	if err != nil {
		return model.Model{}, err
	}

	return m, nil
}

func (p wsdlParser) parseTypes(root *xmlNode) ([]model.Schema, error) {
	var schemas []model.Schema

	for _, child := range root.Children {
		if !hasName(child, wsdlNamespace, "types") {
			continue
		}

		for _, schemaNode := range child.Children {
			if !hasName(schemaNode, xsdNamespace, "schema") {
				continue
			}

			parsed, err := p.parseSchemaNode(schemaNode, p.baseDir)
			if err != nil {
				return nil, err
			}
			schemas = append(schemas, parsed...)
		}
	}

	return schemas, nil
}

func (p wsdlParser) parseSchemaNode(schemaNode *xmlNode, baseDir string) ([]model.Schema, error) {
	if schemaNode == nil {
		return nil, nil
	}

	schema := model.Schema{
		TargetNamespace: attr(schemaNode, "targetNamespace"),
	}

	var imported []model.Schema
	for _, child := range schemaNode.Children {
		switch {
		case hasName(child, xsdNamespace, "element"):
			element, err := parseElement(child)
			if err != nil {
				return nil, err
			}
			schema.Elements = append(schema.Elements, element)
		case hasName(child, xsdNamespace, "complexType"):
			complexType, err := parseComplexType(child)
			if err != nil {
				return nil, err
			}
			schema.ComplexTypes = append(schema.ComplexTypes, complexType)
		case hasName(child, xsdNamespace, "simpleType"):
			simpleType, err := parseSimpleType(child)
			if err != nil {
				return nil, err
			}
			schema.SimpleTypes = append(schema.SimpleTypes, simpleType)
		case hasName(child, xsdNamespace, "include"), hasName(child, xsdNamespace, "import"):
			location := attr(child, "schemaLocation")
			if strings.TrimSpace(location) == "" {
				continue
			}

			fromImport, err := p.parseImportedSchema(baseDir, location)
			if err != nil {
				return nil, err
			}
			imported = append(imported, fromImport...)
		}
	}

	result := []model.Schema{schema}
	result = append(result, imported...)
	return result, nil
}

func (p wsdlParser) parseImportedSchema(baseDir, location string) ([]model.Schema, error) {
	data, nextBaseDir, loaded, err := p.loader.load(baseDir, location)
	if err != nil {
		return nil, fmt.Errorf("load schema location %q: %w", location, err)
	}
	if !loaded {
		return nil, nil
	}

	root, err := parseXMLNode(data)
	if err != nil {
		return nil, fmt.Errorf("parse imported schema %q: %w", location, err)
	}
	if !hasName(root, xsdNamespace, "schema") {
		return nil, fmt.Errorf("schema location %q is not an xsd:schema", location)
	}

	return p.parseSchemaNode(root, nextBaseDir)
}

func parseMessages(root *xmlNode, targetNamespace string) ([]model.Message, error) {
	var messages []model.Message
	for _, node := range root.Children {
		if !hasName(node, wsdlNamespace, "message") {
			continue
		}

		message := model.Message{
			Name: attr(node, "name"),
		}

		for _, partNode := range node.Children {
			if !hasName(partNode, wsdlNamespace, "part") {
				continue
			}

			part := model.MessagePart{
				Name: attr(partNode, "name"),
			}

			var err error
			part.Element, err = parseQName(attr(partNode, "element"), partNode.NS, targetNamespace)
			if err != nil {
				return nil, err
			}
			part.Type, err = parseQName(attr(partNode, "type"), partNode.NS, targetNamespace)
			if err != nil {
				return nil, err
			}

			message.Parts = append(message.Parts, part)
		}

		messages = append(messages, message)
	}

	return messages, nil
}

func parsePortTypeOperations(root *xmlNode, targetNamespace string) (map[string]map[string]portTypeOperation, error) {
	result := make(map[string]map[string]portTypeOperation)

	for _, node := range root.Children {
		if !hasName(node, wsdlNamespace, "portType") {
			continue
		}

		portTypeName := attr(node, "name")
		key := qnameKey(model.QName{Space: targetNamespace, Local: portTypeName})
		if _, exists := result[key]; !exists {
			result[key] = make(map[string]portTypeOperation)
		}

		for _, opNode := range node.Children {
			if !hasName(opNode, wsdlNamespace, "operation") {
				continue
			}

			opName := attr(opNode, "name")
			op := portTypeOperation{}

			inputNode := firstChild(opNode, wsdlNamespace, "input")
			outputNode := firstChild(opNode, wsdlNamespace, "output")

			var err error
			if inputNode != nil {
				op.inputMessage, err = parseQName(attr(inputNode, "message"), inputNode.NS, targetNamespace)
				if err != nil {
					return nil, err
				}
			}
			if outputNode != nil {
				op.outputMessage, err = parseQName(attr(outputNode, "message"), outputNode.NS, targetNamespace)
				if err != nil {
					return nil, err
				}
			}

			result[key][opName] = op
		}
	}

	return result, nil
}

func parseBindings(
	root *xmlNode,
	targetNamespace string,
	portTypeOperations map[string]map[string]portTypeOperation,
) ([]model.Binding, error) {
	var bindings []model.Binding

	for _, node := range root.Children {
		if !hasName(node, wsdlNamespace, "binding") {
			continue
		}

		binding, err := parseBinding(node, targetNamespace, portTypeOperations)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}

	return bindings, nil
}

func parseBinding(
	node *xmlNode,
	targetNamespace string,
	portTypeOperations map[string]map[string]portTypeOperation,
) (model.Binding, error) {
	bindingType, err := parseQName(attr(node, "type"), node.NS, targetNamespace)
	if err != nil {
		return model.Binding{}, err
	}

	binding := model.Binding{
		Name: attr(node, "name"),
		Type: bindingType,
	}

	soapBinding := firstChild(node, soapNamespace, "binding")
	if soapBinding != nil {
		binding.Style = attr(soapBinding, "style")
		binding.Transport = attr(soapBinding, "transport")
	}
	if binding.Style == "" {
		binding.Style = "document"
	}
	if binding.Style != "document" {
		return model.Binding{}, fmt.Errorf("%w: %q", ErrUnsupportedBindingStyle, binding.Style)
	}

	opMessages := findPortTypeOperationMap(portTypeOperations, binding.Type)
	for _, opNode := range node.Children {
		if !hasName(opNode, wsdlNamespace, "operation") {
			continue
		}

		operation, err := parseBindingOperation(opNode, binding.Style, opMessages)
		if err != nil {
			return model.Binding{}, err
		}
		binding.Operations = append(binding.Operations, operation)
	}

	return binding, nil
}

func parseBindingOperation(
	opNode *xmlNode,
	parentStyle string,
	opMessages map[string]portTypeOperation,
) (model.BindingOperation, error) {
	name := attr(opNode, "name")
	operation := model.BindingOperation{
		Name:  name,
		Style: parentStyle,
	}

	if opMessage, ok := opMessages[name]; ok {
		operation.InputMessage = opMessage.inputMessage
		operation.OutputMessage = opMessage.outputMessage
	}

	soapOperation := firstChild(opNode, soapNamespace, "operation")
	if soapOperation != nil {
		operation.Action = attr(soapOperation, "soapAction")
		if style := attr(soapOperation, "style"); style != "" {
			operation.Style = style
		}
	}

	if operation.Style == "" {
		operation.Style = "document"
	}
	if operation.Style != "document" {
		return model.BindingOperation{}, fmt.Errorf("%w: %q", ErrUnsupportedBindingStyle, operation.Style)
	}

	inputNode := firstChild(opNode, wsdlNamespace, "input")
	if inputNode != nil {
		soapBody := firstChild(inputNode, soapNamespace, "body")
		if soapBody != nil {
			operation.InputUse = attr(soapBody, "use")
			operation.InputNamespace = attr(soapBody, "namespace")
		}
	}

	outputNode := firstChild(opNode, wsdlNamespace, "output")
	if outputNode != nil {
		soapBody := firstChild(outputNode, soapNamespace, "body")
		if soapBody != nil {
			operation.OutputUse = attr(soapBody, "use")
		}
	}

	return operation, nil
}

func parseServices(root *xmlNode, targetNamespace string) ([]model.Service, error) {
	var services []model.Service

	for _, serviceNode := range root.Children {
		if !hasName(serviceNode, wsdlNamespace, "service") {
			continue
		}

		service := model.Service{
			Name: attr(serviceNode, "name"),
		}

		for _, portNode := range serviceNode.Children {
			if !hasName(portNode, wsdlNamespace, "port") {
				continue
			}

			binding, err := parseQName(attr(portNode, "binding"), portNode.NS, targetNamespace)
			if err != nil {
				return nil, err
			}

			port := model.Port{
				Name:    attr(portNode, "name"),
				Binding: binding,
			}

			address := firstChild(portNode, soapNamespace, "address")
			if address != nil {
				port.Address = attr(address, "location")
			}

			service.Ports = append(service.Ports, port)
		}

		services = append(services, service)
	}

	return services, nil
}

func parseElement(node *xmlNode) (model.Element, error) {
	elemType, err := parseQName(attr(node, "type"), node.NS, "")
	if err != nil {
		return model.Element{}, err
	}

	return model.Element{
		Name: attr(node, "name"),
		Type: elemType,
	}, nil
}

func parseComplexType(node *xmlNode) (model.ComplexType, error) {
	complexType := model.ComplexType{
		Name: attr(node, "name"),
	}

	for _, child := range node.Children {
		switch {
		case hasName(child, xsdNamespace, "choice"):
			return model.ComplexType{}, ErrUnsupportedXSDChoice
		case hasName(child, xsdNamespace, "sequence"):
			fields, err := parseSequenceFields(child)
			if err != nil {
				return model.ComplexType{}, err
			}
			complexType.Fields = append(complexType.Fields, fields...)
		}
	}

	return complexType, nil
}

func parseSequenceFields(sequenceNode *xmlNode) ([]model.Field, error) {
	fields := make([]model.Field, 0, len(sequenceNode.Children))

	for _, child := range sequenceNode.Children {
		switch {
		case hasName(child, xsdNamespace, "choice"):
			return nil, ErrUnsupportedXSDChoice
		case hasName(child, xsdNamespace, "element"):
			field, err := parseField(child)
			if err != nil {
				return nil, err
			}
			fields = append(fields, field)
		}
	}

	return fields, nil
}

func parseField(node *xmlNode) (model.Field, error) {
	fieldType, err := parseQName(attr(node, "type"), node.NS, "")
	if err != nil {
		return model.Field{}, err
	}

	minOccurs, err := parseOccurs(attr(node, "minOccurs"), 1)
	if err != nil {
		return model.Field{}, err
	}
	maxOccurs, err := parseMaxOccurs(attr(node, "maxOccurs"))
	if err != nil {
		return model.Field{}, err
	}

	return model.Field{
		Name:      attr(node, "name"),
		Type:      fieldType,
		MinOccurs: minOccurs,
		MaxOccurs: maxOccurs,
		Nillable:  parseBool(attr(node, "nillable")),
	}, nil
}

func parseSimpleType(node *xmlNode) (model.SimpleType, error) {
	simpleType := model.SimpleType{
		Name: attr(node, "name"),
	}

	restriction := firstChild(node, xsdNamespace, "restriction")
	if restriction == nil {
		return simpleType, nil
	}

	base, err := parseQName(attr(restriction, "base"), restriction.NS, "")
	if err != nil {
		return model.SimpleType{}, err
	}
	simpleType.Base = base

	return simpleType, nil
}

func parseXMLNode(data []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var root *xmlNode
	stack := make([]*xmlNode, 0, 8)

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}

		switch tok := token.(type) {
		case xml.StartElement:
			node := &xmlNode{
				Name: tok.Name,
				Attr: append([]xml.Attr(nil), tok.Attr...),
			}

			if len(stack) > 0 {
				node.NS = cloneNamespace(stack[len(stack)-1].NS)
			} else {
				node.NS = make(map[string]string)
			}
			applyNamespaceDecl(node.NS, tok.Attr)

			if len(stack) == 0 {
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			stack = stack[:len(stack)-1]
		}
	}

	if root == nil {
		return nil, errors.New("empty xml document")
	}

	return root, nil
}

func applyNamespaceDecl(target map[string]string, attrs []xml.Attr) {
	for _, attr := range attrs {
		switch {
		case attr.Name.Space == "xmlns":
			target[attr.Name.Local] = attr.Value
		case attr.Name.Space == "" && attr.Name.Local == "xmlns":
			target[""] = attr.Value
		}
	}
}

func cloneNamespace(ns map[string]string) map[string]string {
	out := make(map[string]string, len(ns))
	for k, v := range ns {
		out[k] = v
	}
	return out
}

func parseQName(value string, ns map[string]string, fallbackNamespace string) (model.QName, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return model.QName{}, nil
	}

	parts := strings.SplitN(value, ":", 2)
	if len(parts) == 2 {
		prefix := strings.TrimSpace(parts[0])
		local := strings.TrimSpace(parts[1])

		namespace, ok := ns[prefix]
		if !ok {
			return model.QName{}, fmt.Errorf("undefined namespace prefix %q in qname %q", prefix, value)
		}

		return model.QName{
			Space: namespace,
			Local: local,
		}, nil
	}

	local := parts[0]
	if namespace, ok := ns[""]; ok && namespace != "" {
		return model.QName{
			Space: namespace,
			Local: local,
		}, nil
	}

	if fallbackNamespace != "" {
		return model.QName{
			Space: fallbackNamespace,
			Local: local,
		}, nil
	}

	return model.QName{
		Local: local,
	}, nil
}

func parseOccurs(value string, defaultValue int) (int, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid occurs value %q: %w", value, err)
	}
	return parsed, nil
}

func parseMaxOccurs(value string) (int, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 1, nil
	}

	if v == "unbounded" {
		return model.MaxOccursUnbounded, nil
	}

	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid maxOccurs %q: %w", value, err)
	}
	return parsed, nil
}

func parseBool(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	return v == "true" || v == "1"
}

func hasName(node *xmlNode, namespace, local string) bool {
	if node == nil {
		return false
	}

	if node.Name.Local != local {
		return false
	}

	return namespace == "" || node.Name.Space == namespace
}

func firstChild(node *xmlNode, namespace, local string) *xmlNode {
	for _, child := range node.Children {
		if hasName(child, namespace, local) {
			return child
		}
	}

	return nil
}

func attr(node *xmlNode, key string) string {
	for _, at := range node.Attr {
		if at.Name.Space == "" && at.Name.Local == key {
			return strings.TrimSpace(at.Value)
		}
	}

	return ""
}

func findPortTypeOperationMap(
	portTypeOperations map[string]map[string]portTypeOperation,
	bindingType model.QName,
) map[string]portTypeOperation {
	if operations, ok := portTypeOperations[qnameKey(bindingType)]; ok {
		return operations
	}

	for key, operations := range portTypeOperations {
		if strings.HasSuffix(key, "|"+bindingType.Local) {
			return operations
		}
	}

	return nil
}

func qnameKey(name model.QName) string {
	return name.Space + "|" + name.Local
}

func mergeSchemasByTargetNamespace(schemas []model.Schema) []model.Schema {
	indexByNamespace := make(map[string]int)
	merged := make([]model.Schema, 0, len(schemas))

	for _, schema := range schemas {
		idx, ok := indexByNamespace[schema.TargetNamespace]
		if !ok {
			indexByNamespace[schema.TargetNamespace] = len(merged)
			merged = append(merged, schema)
			continue
		}

		merged[idx].Elements = append(merged[idx].Elements, schema.Elements...)
		merged[idx].ComplexTypes = append(merged[idx].ComplexTypes, schema.ComplexTypes...)
		merged[idx].SimpleTypes = append(merged[idx].SimpleTypes, schema.SimpleTypes...)
	}

	return merged
}

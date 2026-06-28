package model

const MaxOccursUnbounded = -1

type QName struct {
	Space string
	Local string
}

type Model struct {
	TargetNamespace string
	Schemas         []Schema
	Messages        []Message
	Bindings        []Binding
	Services        []Service
}

type Schema struct {
	TargetNamespace string
	Elements        []Element
	ComplexTypes    []ComplexType
	SimpleTypes     []SimpleType
}

type Element struct {
	Name string
	Type QName
}

type ComplexType struct {
	Name   string
	Fields []Field
}

type SimpleType struct {
	Name       string
	Base       QName
	Enums      []string
	Pattern    string
	MinLength  *int
	MaxLength  *int
	MinValue   *int64
	MaxValue   *int64
	IsListType bool
}

type Field struct {
	Name      string
	Type      QName
	MinOccurs int
	MaxOccurs int
	Nillable  bool
}

type Message struct {
	Name  string
	Parts []MessagePart
}

type MessagePart struct {
	Name    string
	Element QName
	Type    QName
}

type Binding struct {
	Name       string
	Type       QName
	Transport  string
	Style      string
	Operations []BindingOperation
}

type BindingOperation struct {
	Name           string
	Action         string
	Style          string
	InputMessage   QName
	OutputMessage  QName
	InputUse       string
	OutputUse      string
	InputNamespace string
}

type Service struct {
	Name  string
	Ports []Port
}

type Port struct {
	Name    string
	Binding QName
	Address string
}
